package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// ListChains returns all chains with derived active state and last run status
// (REQ-002). It does not filter by client_name so the full fleet is visible.
func (c *PgClient) ListChains(ctx context.Context) ([]ChainListItem, error) {
	const q = `
SELECT
    c.chain_id,
    c.chain_name,
    c.self_destruct,
    c.exclusive_execution,
    COALESCE(c.max_instances, 0)        AS max_instances,
    COALESCE(c.timeout, 0)              AS timeout,
    COALESCE(c.on_error, '')            AS on_error,
    COALESCE(c.client_name, '')         AS client_name,
    COALESCE(c.run_at, '')              AS run_at,
    COALESCE(c.live, FALSE)             AS live,
    EXISTS (SELECT 1 FROM timetable.active_chain ac WHERE ac.chain_id = c.chain_id) AS active,
    COALESCE((
        SELECT CASE
                   WHEN el.finished IS NULL THEN 'running'
                   WHEN el.returncode = 0   THEN 'success'
                   ELSE 'failed'
               END
        FROM timetable.execution_log el
        WHERE el.chain_id = c.chain_id
        ORDER BY el.last_run DESC
        LIMIT 1
    ), '') AS last_status
FROM timetable.chain c
ORDER BY c.chain_id`
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[ChainListItem])
}

// ShowChain returns a single chain (by numeric id or name) and its ordered tasks
// (REQ-003).
func (c *PgClient) ShowChain(ctx context.Context, ref string) (*ChainListItem, []ChainTask, error) {
	const qByID = `
SELECT
    c.chain_id, c.chain_name, c.self_destruct, c.exclusive_execution,
    COALESCE(c.max_instances, 0) AS max_instances, COALESCE(c.timeout, 0) AS timeout,
    COALESCE(c.on_error, '') AS on_error, COALESCE(c.client_name, '') AS client_name,
    COALESCE(c.run_at, '') AS run_at, COALESCE(c.live, FALSE) AS live,
    EXISTS (SELECT 1 FROM timetable.active_chain ac WHERE ac.chain_id = c.chain_id) AS active,
    '' AS last_status
FROM timetable.chain c
WHERE %s`

	var (
		rows pgx.Rows
		err  error
	)
	if id, convErr := strconv.Atoi(ref); convErr == nil {
		rows, err = c.pool.Query(ctx, fmt.Sprintf(qByID, "c.chain_id = $1"), id)
	} else {
		rows, err = c.pool.Query(ctx, fmt.Sprintf(qByID, "c.chain_name = $1"), ref)
	}
	if err != nil {
		return nil, nil, err
	}
	chain, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[ChainListItem])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, fmt.Errorf("chain %q not found", ref)
		}
		return nil, nil, err
	}

	const qTasks = `
SELECT
    task_id,
    COALESCE(task_name, '') AS task_name,
    command,
    kind,
    COALESCE(run_as, '') AS run_as,
    ignore_error,
    autonomous,
    COALESCE(database_connection, '') AS database_connection,
    COALESCE(timeout, 0) AS timeout
FROM timetable.task
WHERE chain_id = $1
ORDER BY task_order ASC`
	trows, err := c.pool.Query(ctx, qTasks, chain.ChainID)
	if err != nil {
		return nil, nil, err
	}
	tasks, err := pgx.CollectRows(trows, pgx.RowToStructByName[ChainTask])
	if err != nil {
		return nil, nil, err
	}
	return &chain, tasks, nil
}

// ListSessions returns active scheduler sessions (REQ-011).
func (c *PgClient) ListSessions(ctx context.Context) ([]Session, error) {
	const q = `
SELECT client_name, client_pid, server_pid,
       COALESCE(to_char(started_at, 'YYYY-MM-DD HH24:MI:SS'), '') AS started_at
FROM timetable.active_session
ORDER BY started_at`
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Session])
}

// ListActiveChains returns currently running chains (REQ-011).
func (c *PgClient) ListActiveChains(ctx context.Context) ([]ActiveChain, error) {
	const q = `
SELECT chain_id, client_name,
       COALESCE(to_char(started_at, 'YYYY-MM-DD HH24:MI:SS'), '') AS started_at
FROM timetable.active_chain
ORDER BY started_at`
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[ActiveChain])
}

// ListLogs returns log entries, optionally filtered by chain and client (REQ-012).
// A chain filter matches log rows whose message_data references the chain id.
func (c *PgClient) ListLogs(ctx context.Context, f LogFilter) ([]LogEntry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	q := `
SELECT to_char(ts, 'YYYY-MM-DD HH24:MI:SS') AS ts,
       pid,
       log_level::text AS log_level,
       COALESCE(client_name, '') AS client_name,
       COALESCE(message, '') AS message
FROM timetable.log
WHERE ($1 = '' OR client_name = $1)
  AND ($2 = 0  OR (message_data->>'chain')::bigint = $2)
ORDER BY ts DESC
LIMIT $3`
	rows, err := c.pool.Query(ctx, q, f.ClientName, f.ChainID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[LogEntry])
}
