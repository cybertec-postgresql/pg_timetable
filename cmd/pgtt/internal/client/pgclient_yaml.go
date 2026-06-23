package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"
)

// exportWarningHeader is prepended to every exported YAML document. Export is a
// best-effort static snapshot (REQ-010 / spec §9): it cannot reproduce chains
// that are programmatically generated or modify their own tasks/parameters at
// runtime.
const exportWarningHeader = `# pgtt export — best-effort static snapshot.
# WARNING: This captures chain/task/parameter rows AS THEY CURRENTLY EXIST.
# It does NOT capture imperative chain-construction logic or runtime
# self-modification (e.g. a task that rewrites another task's parameter, or
# commands embedding literal task_id/chain_id values). Re-importing such a
# chain may produce a different or broken chain. Review before applying.
`

// ApplyYAML imports chains from a YAML file (REQ-009). It reuses the core
// pgengine YAML parser/validator, then inserts via the same column layout as
// pgengine.CreateChainFromYaml. With replace=true an existing chain of the same
// name is deleted first. Returns the number of chains imported.
func (c *PgClient) ApplyYAML(ctx context.Context, path string, replace bool) (int, error) {
	cfg, err := pgengine.ParseYamlFile(path)
	if err != nil {
		return 0, err
	}
	count := 0
	for i := range cfg.Chains {
		yc := &cfg.Chains[i]
		if err := yc.ValidateChain(); err != nil {
			return count, fmt.Errorf("chain %q: %w", yc.ChainName, err)
		}
		yc.SetDefaults()

		if replace {
			_, _ = c.pool.Exec(ctx, `SELECT timetable.delete_job($1)`, yc.ChainName)
		} else {
			var exists bool
			if err := c.pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM timetable.chain WHERE chain_name = $1)`,
				yc.ChainName).Scan(&exists); err != nil {
				return count, err
			}
			if exists {
				return count, fmt.Errorf("chain %q already exists (use --replace to overwrite)", yc.ChainName)
			}
		}
		if err := c.insertYamlChain(ctx, yc); err != nil {
			return count, fmt.Errorf("importing chain %q: %w", yc.ChainName, err)
		}
		count++
	}
	return count, nil
}

func (c *PgClient) insertYamlChain(ctx context.Context, yc *pgengine.YamlChain) error {
	var chainID int64
	err := c.pool.QueryRow(ctx, `
INSERT INTO timetable.chain
    (chain_name, run_at, max_instances, timeout, live, self_destruct, exclusive_execution, client_name, on_error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING chain_id`,
		yc.ChainName, yc.Schedule, nullableInt(yc.MaxInstances), yc.Timeout, yc.Live,
		yc.SelfDestruct, yc.ExclusiveExecution, nullString(yc.ClientName), nullString(yc.OnError),
	).Scan(&chainID)
	if err != nil {
		return err
	}
	for i, task := range yc.Tasks {
		var taskID int64
		err := c.pool.QueryRow(ctx, `
INSERT INTO timetable.task
    (chain_id, task_order, task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout)
VALUES ($1, $2, $3, $4::timetable.command_kind, $5, $6, $7, $8, $9, $10)
RETURNING task_id`,
			chainID, float64((i+1)*10), nullString(task.TaskName), defaultKind(task.Kind), task.Command,
			nullString(task.RunAs), nullString(task.ConnectString), task.IgnoreError, task.Autonomous, task.Timeout,
		).Scan(&taskID)
		if err != nil {
			return err
		}
		for pi, param := range task.Parameters {
			jsonValue, err := json.Marshal(param)
			if err != nil {
				return err
			}
			if _, err := c.pool.Exec(ctx,
				`INSERT INTO timetable.parameter (task_id, order_id, value) VALUES ($1, $2, $3::jsonb)`,
				taskID, pi+1, string(jsonValue)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ExportYAML serializes the referenced chains to YAML (REQ-010). It always
// succeeds for existing chains and returns a warning slice for the caller to
// surface (best-effort snapshot; see exportWarningHeader).
func (c *PgClient) ExportYAML(ctx context.Context, refs []string) ([]byte, []string, error) {
	cfg := pgengine.YamlConfig{}
	warnings := []string{}
	for _, ref := range refs {
		id, err := c.resolveChainID(ctx, ref)
		if err != nil {
			return nil, nil, err
		}
		yc, err := c.exportChain(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		cfg.Chains = append(cfg.Chains, *yc)
		warnings = append(warnings,
			fmt.Sprintf("chain %q exported as a static snapshot; verify it is not self-modifying before re-applying", yc.ChainName))
	}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}
	out := append([]byte(exportWarningHeader), body...)
	return out, warnings, nil
}

func (c *PgClient) exportChain(ctx context.Context, chainID int) (*pgengine.YamlChain, error) {
	var yc pgengine.YamlChain
	err := c.pool.QueryRow(ctx, `
SELECT chain_name, COALESCE(run_at, '') , COALESCE(max_instances, 0), COALESCE(timeout, 0),
       COALESCE(live, FALSE), self_destruct, exclusive_execution, COALESCE(on_error, ''), COALESCE(client_name, '')
FROM timetable.chain WHERE chain_id = $1`, chainID).Scan(
		&yc.ChainName, &yc.Schedule, &yc.MaxInstances, &yc.Timeout,
		&yc.Live, &yc.SelfDestruct, &yc.ExclusiveExecution, &yc.OnError, &yc.ClientName)
	if err != nil {
		return nil, err
	}

	rows, err := c.pool.Query(ctx, `
SELECT task_id, COALESCE(task_name, ''), command, kind::text, COALESCE(run_as, ''),
       ignore_error, autonomous, COALESCE(database_connection, ''), COALESCE(timeout, 0)
FROM timetable.task WHERE chain_id = $1 ORDER BY task_order ASC`, chainID)
	if err != nil {
		return nil, err
	}
	type taskRow struct {
		ID                                  int
		Name, Command, Kind, RunAs, ConnStr string
		IgnoreError, Autonomous             bool
		Timeout                             int
	}
	taskRows, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (taskRow, error) {
		var t taskRow
		err := r.Scan(&t.ID, &t.Name, &t.Command, &t.Kind, &t.RunAs,
			&t.IgnoreError, &t.Autonomous, &t.ConnStr, &t.Timeout)
		return t, err
	})
	if err != nil {
		return nil, err
	}

	for _, t := range taskRows {
		yt := pgengine.YamlTask{
			TaskName: t.Name,
		}
		yt.Command = t.Command
		yt.Kind = strings.ToUpper(t.Kind)
		yt.RunAs = t.RunAs
		yt.ConnectString = t.ConnStr
		yt.IgnoreError = t.IgnoreError
		yt.Autonomous = t.Autonomous
		yt.Timeout = t.Timeout

		params, err := c.exportParams(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		yt.Parameters = params
		yc.Tasks = append(yc.Tasks, yt)
	}
	return &yc, nil
}

func (c *PgClient) exportParams(ctx context.Context, taskID int) ([]any, error) {
	rows, err := c.pool.Query(ctx,
		`SELECT value FROM timetable.parameter WHERE task_id = $1 ORDER BY order_id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	raws, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(raws))
	for _, raw := range raws {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
