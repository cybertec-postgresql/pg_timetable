package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// ListChains returns all chains with derived active state and enriched last-run
// data from timetable.execution_log (REQ-002 / P5-3).
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
    COALESCE(c.run_at, '* * * * *')     AS run_at,
    COALESCE(c.live, FALSE)             AS live,
    EXISTS (SELECT 1 FROM timetable.active_chain ac WHERE ac.chain_id = c.chain_id) AS active,
    COALESCE(lr.status, '')             AS last_status,
    COALESCE(lr.last_run, '')           AS last_run,
    COALESCE(lr.duration_ms, 0)         AS last_duration_ms,
    COALESCE(lr.returncode, 0)          AS last_returncode,
    COALESCE(lr.client_name, '')        AS last_worker
FROM timetable.chain c
LEFT JOIN LATERAL (
    SELECT
        CASE
            WHEN el.finished IS NULL THEN 'running'
            WHEN el.returncode = 0   THEN 'success'
            ELSE 'failed'
        END                                                         AS status,
        to_char(el.last_run, 'YYYY-MM-DD HH24:MI:SS')                   AS last_run,
        (EXTRACT(EPOCH FROM (el.finished - el.last_run)) * 1000)::bigint AS duration_ms,
        COALESCE(el.returncode, 0)                                       AS returncode,
        el.client_name
    FROM timetable.execution_log el
    WHERE el.chain_id = c.chain_id
    ORDER BY el.last_run DESC
    LIMIT 1
) lr ON TRUE
ORDER BY c.chain_id`
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[ChainListItem])
}

// ShowChain returns a single chain (by numeric id or name) and its ordered tasks
// (REQ-003), enriched with last-run data from execution_log (P5-3).
func (c *PgClient) ShowChain(ctx context.Context, ref string) (*ChainListItem, []ChainTask, error) {
	const qByID = `
SELECT
    c.chain_id, c.chain_name, c.self_destruct, c.exclusive_execution,
    COALESCE(c.max_instances, 0) AS max_instances, COALESCE(c.timeout, 0) AS timeout,
    COALESCE(c.on_error, '') AS on_error, COALESCE(c.client_name, '') AS client_name,
    COALESCE(c.run_at, '* * * * *') AS run_at, COALESCE(c.live, FALSE) AS live,
    EXISTS (SELECT 1 FROM timetable.active_chain ac WHERE ac.chain_id = c.chain_id) AS active,
    COALESCE(lr.status, '')      AS last_status,
    COALESCE(lr.last_run, '')    AS last_run,
    COALESCE(lr.duration_ms, 0) AS last_duration_ms,
    COALESCE(lr.returncode, 0)  AS last_returncode,
    COALESCE(lr.worker, '')     AS last_worker
FROM timetable.chain c
LEFT JOIN LATERAL (
    SELECT
        CASE WHEN el.finished IS NULL THEN 'running'
             WHEN el.returncode = 0   THEN 'success' ELSE 'failed' END AS status,
        to_char(el.last_run, 'YYYY-MM-DD HH24:MI:SS')                    AS last_run,
        (EXTRACT(EPOCH FROM (el.finished - el.last_run)) * 1000)::bigint  AS duration_ms,
        COALESCE(el.returncode, 0)                                        AS returncode,
        el.client_name                                                    AS worker
    FROM timetable.execution_log el WHERE el.chain_id = c.chain_id
    ORDER BY el.last_run DESC LIMIT 1
) lr ON TRUE
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

// ListRuns returns recent execution runs for a chain, one row per txid (P5-4).
func (c *PgClient) ListRuns(ctx context.Context, ref string, limit int) ([]RunSummary, error) {
	id, err := c.resolveChainID(ctx, ref)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	const q = `
SELECT
    txid,
    to_char(MIN(last_run), 'YYYY-MM-DD HH24:MI:SS')                    AS started_at,
    to_char(MAX(finished),  'YYYY-MM-DD HH24:MI:SS')                    AS finished_at,
    COALESCE(
        EXTRACT(EPOCH FROM (MAX(finished) - MIN(last_run))) * 1000, 0
    )::bigint                                                            AS duration_ms,
    CASE
        WHEN bool_and(finished IS NOT NULL) AND
             bool_and(returncode = 0 OR ignore_error) THEN 'success'
        WHEN bool_or(finished IS NULL)                THEN 'running'
        ELSE 'failed'
    END                                                                  AS status,
    MIN(client_name)                                                     AS client_name,
    COUNT(*)::int                                                        AS total_tasks,
    COUNT(*) FILTER (WHERE returncode <> 0 AND NOT ignore_error)::int   AS failed_tasks
FROM timetable.execution_log
WHERE chain_id = $1
GROUP BY txid
ORDER BY MIN(last_run) DESC
LIMIT $2`
	rows, err := c.pool.Query(ctx, q, id, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[RunSummary])
}

// ShowRun returns per-task detail for a single txid (P5-5).
func (c *PgClient) ShowRun(ctx context.Context, txid int64) ([]RunTaskDetail, error) {
	const q = `
SELECT
    COALESCE(task_id, 0)                                                 AS task_id,
    COALESCE(kind::text, '')                                             AS kind,
    COALESCE(command, '')                                                AS command,
    to_char(last_run, 'YYYY-MM-DD HH24:MI:SS')                          AS started_at,
    COALESCE(to_char(finished, 'YYYY-MM-DD HH24:MI:SS'), '')            AS finished_at,
    COALESCE(
        EXTRACT(EPOCH FROM (finished - last_run)) * 1000, 0
    )::bigint                                                            AS duration_ms,
    COALESCE(returncode, 0)                                             AS returncode,
    COALESCE(ignore_error, FALSE)                                       AS ignore_error,
    COALESCE(params, '')                                                AS params,
    COALESCE(output, '')                                                AS output
FROM timetable.execution_log
WHERE txid = $1
ORDER BY last_run ASC`
	rows, err := c.pool.Query(ctx, q, txid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[RunTaskDetail])
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
  AND ($2 = 0  OR (message_data->'chain'->>'ChainID')::bigint = $2)
ORDER BY ts DESC
LIMIT $3`
	rows, err := c.pool.Query(ctx, q, f.ClientName, f.ChainID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[LogEntry])
}
