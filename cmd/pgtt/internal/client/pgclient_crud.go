package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// nullString returns nil for empty strings so they map to SQL NULL.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// resolveChainID converts a chain reference (numeric id or unique name) to its id.
func (c *PgClient) resolveChainID(ctx context.Context, ref string) (int, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		return id, nil
	}
	var id int
	err := c.pool.QueryRow(ctx,
		`SELECT chain_id FROM timetable.chain WHERE chain_name = $1`, ref).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("chain %q not found", ref)
		}
		return 0, err
	}
	return id, nil
}

func defaultKind(kind string) string {
	if kind == "" {
		return "SQL"
	}
	return strings.ToUpper(kind)
}

// CreateChain creates a new chain with a single initial task (REQ-004).
func (c *PgClient) CreateChain(ctx context.Context, spec ChainSpec) (int, error) {
	if spec.Name == "" {
		return 0, fmt.Errorf("chain name is required")
	}
	if spec.Schedule == "" {
		return 0, fmt.Errorf("chain schedule is required")
	}
	if spec.Command == "" {
		return 0, fmt.Errorf("initial task command is required")
	}
	var chainID int
	err := c.pool.QueryRow(ctx, `
INSERT INTO timetable.chain
    (chain_name, run_at, max_instances, live, self_destruct, exclusive_execution, client_name, on_error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING chain_id`,
		spec.Name, spec.Schedule, nullableInt(spec.MaxInstances), spec.Live,
		spec.SelfDestruct, spec.Exclusive, nullString(spec.ClientName), nullString(spec.OnError),
	).Scan(&chainID)
	if err != nil {
		return 0, fmt.Errorf("creating chain: %w", err)
	}
	_, err = c.pool.Exec(ctx, `
INSERT INTO timetable.task (chain_id, task_order, kind, command)
VALUES ($1, 10, $2::timetable.command_kind, $3)`,
		chainID, defaultKind(spec.Kind), spec.Command)
	if err != nil {
		return 0, fmt.Errorf("creating initial task: %w", err)
	}
	return chainID, nil
}

// EditChain updates chain attributes; only non-nil fields change (REQ-004).
func (c *PgClient) EditChain(ctx context.Context, ref string, spec ChainEdit) error {
	id, err := c.resolveChainID(ctx, ref)
	if err != nil {
		return err
	}
	set := make([]string, 0, 7)
	args := make([]any, 0, 8)
	n := 1
	add := func(col string, v any) {
		set = append(set, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, v)
		n++
	}
	if spec.Schedule != nil {
		add("run_at", *spec.Schedule)
	}
	if spec.ClientName != nil {
		add("client_name", nullString(*spec.ClientName))
	}
	if spec.MaxInstances != nil {
		add("max_instances", *spec.MaxInstances)
	}
	if spec.Live != nil {
		add("live", *spec.Live)
	}
	if spec.SelfDestruct != nil {
		add("self_destruct", *spec.SelfDestruct)
	}
	if spec.Exclusive != nil {
		add("exclusive_execution", *spec.Exclusive)
	}
	if spec.OnError != nil {
		add("on_error", nullString(*spec.OnError))
	}
	if len(set) == 0 {
		return fmt.Errorf("nothing to update: provide at least one field")
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE timetable.chain SET %s WHERE chain_id = $%d", strings.Join(set, ", "), n)
	tag, err := c.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("chain %q not found", ref)
	}
	return nil
}

// DeleteChain removes a chain and its tasks (REQ-004). Confirmation is enforced
// by the command layer (SEC-003); this method performs the deletion.
func (c *PgClient) DeleteChain(ctx context.Context, ref string) error {
	id, err := c.resolveChainID(ctx, ref)
	if err != nil {
		return err
	}
	tag, err := c.pool.Exec(ctx, `DELETE FROM timetable.chain WHERE chain_id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("chain %q not found", ref)
	}
	return nil
}

// AddTask appends a task to a chain after the current highest task_order (REQ-004).
func (c *PgClient) AddTask(ctx context.Context, chainRef string, spec TaskSpec) (int, error) {
	if spec.Command == "" {
		return 0, fmt.Errorf("task command is required")
	}
	id, err := c.resolveChainID(ctx, chainRef)
	if err != nil {
		return 0, err
	}
	var taskID int
	err = c.pool.QueryRow(ctx, `
INSERT INTO timetable.task
    (chain_id, task_order, task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout)
VALUES (
    $1,
    COALESCE((SELECT max(task_order) FROM timetable.task WHERE chain_id = $1), 0) + 10,
    $2, $3::timetable.command_kind, $4, $5, $6, $7, $8, $9
)
RETURNING task_id`,
		id, nullString(spec.Name), defaultKind(spec.Kind), spec.Command,
		nullString(spec.RunAs), nullString(spec.ConnectStr), spec.IgnoreError, spec.Autonomous, spec.Timeout,
	).Scan(&taskID)
	if err != nil {
		return 0, fmt.Errorf("adding task: %w", err)
	}
	return taskID, nil
}

// EditTask updates task attributes; only non-nil fields change (REQ-004).
func (c *PgClient) EditTask(ctx context.Context, taskID int, spec TaskEdit) error {
	set := make([]string, 0, 8)
	args := make([]any, 0, 9)
	n := 1
	add := func(col string, v any) {
		set = append(set, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, v)
		n++
	}
	if spec.Name != nil {
		add("task_name", nullString(*spec.Name))
	}
	if spec.Kind != nil {
		set = append(set, fmt.Sprintf("kind = $%d::timetable.command_kind", n))
		args = append(args, defaultKind(*spec.Kind))
		n++
	}
	if spec.Command != nil {
		add("command", *spec.Command)
	}
	if spec.RunAs != nil {
		add("run_as", nullString(*spec.RunAs))
	}
	if spec.ConnectStr != nil {
		add("database_connection", nullString(*spec.ConnectStr))
	}
	if spec.IgnoreError != nil {
		add("ignore_error", *spec.IgnoreError)
	}
	if spec.Autonomous != nil {
		add("autonomous", *spec.Autonomous)
	}
	if spec.Timeout != nil {
		add("timeout", *spec.Timeout)
	}
	if len(set) == 0 {
		return fmt.Errorf("nothing to update: provide at least one field")
	}
	args = append(args, taskID)
	q := fmt.Sprintf("UPDATE timetable.task SET %s WHERE task_id = $%d", strings.Join(set, ", "), n)
	tag, err := c.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %d not found", taskID)
	}
	return nil
}

// DeleteTask removes a task via timetable.delete_task (REQ-004).
func (c *PgClient) DeleteTask(ctx context.Context, taskID int) error {
	var ok bool
	if err := c.pool.QueryRow(ctx, `SELECT timetable.delete_task($1)`, taskID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("task %d not found", taskID)
	}
	return nil
}

// MoveTask reorders a task within its chain via move_task_up/down (REQ-008).
func (c *PgClient) MoveTask(ctx context.Context, taskID int, up bool) error {
	fn := "timetable.move_task_down"
	if up {
		fn = "timetable.move_task_up"
	}
	var ok bool
	if err := c.pool.QueryRow(ctx, fmt.Sprintf("SELECT %s($1)", fn), taskID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("could not move task %d (already at boundary or not found)", taskID)
	}
	return nil
}

// nullableInt returns nil for 0 (meaning "unset"), else the value.
func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
