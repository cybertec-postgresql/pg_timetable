package client

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// activitySQL is the unified UNION ALL query merging timetable.log (scheduler
// diagnostics) with timetable.execution_log (task execution results) into one
// chronological activity stream. Both sources are filtered by the same
// client_name and chain_id parameters.
//
// execution_log rows have no "message" column; we synthesise one from the
// returncode and truncated output so the feed is readable in a single column.
//
// The scheduler stores rich structured context in timetable.log.message_data
// (jsonb): the "chain" object (ChainID/ChainName), the "task" object
// (TaskID/TaskName), and a top-level "vxid". We mine those keys here so log
// rows are no longer rendered as a contextless wall of zeros. Keys use Go field
// names because the scheduler marshals its structs without json tags.
const activitySQL = `
SELECT
    to_char(ts, 'YYYY-MM-DD HH24:MI:SS.MS') AS ts,
    'log'                                    AS source,
    COALESCE(client_name, '')                AS client_name,
    COALESCE((message_data->'chain'->>'ChainID')::bigint, 0)         AS chain_id,
    COALESCE(message_data->'chain'->>'ChainName', '')               AS chain_name,
    COALESCE((message_data->'task'->>'TaskID')::bigint, 0)          AS task_id,
    COALESCE(message_data->>'vxid', '')                             AS vxid,
    COALESCE(NULLIF(message_data->>'severity', ''), log_level::text) AS level,
    0                                        AS returncode,
    0::bigint                                AS duration_ms,
    COALESCE(message, '')                    AS message,
    ''                                       AS command,
    COALESCE(message_data->>'notice', '')    AS notice,
    COALESCE(message_data->>'severity', '')  AS severity,
    false                                    AS is_header
FROM timetable.log
WHERE ($1 = '' OR client_name = $1)
  AND ($2 = 0  OR (message_data->'chain'->>'ChainID')::bigint = $2)

UNION ALL

SELECT
    to_char(last_run, 'YYYY-MM-DD HH24:MI:SS.MS')         AS ts,
    'exec'                                                 AS source,
    client_name,
    COALESCE(chain_id, 0)                                  AS chain_id,
    ''                                                     AS chain_name,
    COALESCE(task_id, 0)                                   AS task_id,
    COALESCE(txid::text, '')                               AS vxid,
    CASE WHEN finished IS NULL       THEN 'RUNNING'
         WHEN returncode = 0        THEN 'OK'
         WHEN ignore_error          THEN 'WARN'
         ELSE                            'FAIL'
    END                                                    AS level,
    COALESCE(returncode, 0)                                AS returncode,
    (EXTRACT(EPOCH FROM (finished - last_run)) * 1000)::bigint AS duration_ms,
    CASE
        WHEN output IS NULL OR output = '' THEN ''
        ELSE left(output, 200)
    END                                                    AS message,
    COALESCE(command, '')                                  AS command,
    ''                                                     AS notice,
    ''                                                     AS severity,
    false                                                  AS is_header
FROM timetable.execution_log
WHERE ($1 = '' OR client_name = $1)
  AND ($2 = 0  OR chain_id = $2)

ORDER BY ts DESC`

// ListActivity returns a merged chronological activity feed (REQ-012).
func (c *PgClient) ListActivity(ctx context.Context, f LogFilter) ([]ActivityEntry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	q := activitySQL + "\nLIMIT $3"
	rows, err := c.pool.Query(ctx, q, f.ClientName, f.ChainID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[ActivityEntry])
}

// activityTreeSQL groups the unified feed by chain run (chain + client +
// virtual transaction id) entirely in SQL, so the renderer needs no grouping
// logic. The pipeline:
//
//	feed   – the same UNION ALL as activitySQL, with a raw ts and a tree-rank
//	         hint, limited to the most recent $3 rows.
//	runseq – a running count of "Starting chain" lines per chain+client. Each
//	         chain start opens a new run, so all rows accumulate into the most
//	         recent run. This is a temporal grouping that does not rely on the
//	         vxid being present on the start line (it is not) and never merges
//	         distinct runs of the same chain.
//	runs   – broadcast the run's real vxid (carried by task/exec lines) onto
//	         the vxid-less lines (e.g. "Starting chain") of the same run.
//	ranked – mark the run's first line (Starting chain, else earliest) as the
//	         header, and compute each run's latest ts for newest-first ordering.
//
// Final ORDER BY: chain-less scheduler rows last; otherwise newest run first,
// and within a run "Starting chain" first then chronological.
const activityTreeSQL = `
WITH feed AS (
    SELECT
        ts,
        to_char(ts, 'YYYY-MM-DD HH24:MI:SS.MS') AS ts_str,
        'log'                                    AS source,
        COALESCE(client_name, '')                AS client_name,
        COALESCE((message_data->'chain'->>'ChainID')::bigint, 0)         AS chain_id,
        COALESCE(message_data->'chain'->>'ChainName', '')               AS chain_name,
        COALESCE((message_data->'task'->>'TaskID')::bigint, 0)          AS task_id,
        COALESCE(message_data->>'vxid', '')                             AS vxid,
        COALESCE(NULLIF(message_data->>'severity', ''), log_level::text) AS level,
        0                                        AS returncode,
        0::bigint                                AS duration_ms,
        COALESCE(message, '')                    AS message,
        ''                                       AS command,
        COALESCE(message_data->>'notice', '')    AS notice,
        COALESCE(message_data->>'severity', '')  AS severity
    FROM timetable.log
    WHERE ($1 = '' OR client_name = $1)
      AND ($2 = 0  OR (message_data->'chain'->>'ChainID')::bigint = $2)

    UNION ALL

    SELECT
        last_run                                               AS ts,
        to_char(last_run, 'YYYY-MM-DD HH24:MI:SS.MS')         AS ts_str,
        'exec'                                                 AS source,
        client_name,
        COALESCE(chain_id, 0)                                  AS chain_id,
        ''                                                     AS chain_name,
        COALESCE(task_id, 0)                                   AS task_id,
        COALESCE(txid::text, '')                               AS vxid,
        CASE WHEN finished IS NULL THEN 'RUNNING'
             WHEN returncode = 0   THEN 'OK'
             WHEN ignore_error     THEN 'WARN'
             ELSE                       'FAIL' END             AS level,
        COALESCE(returncode, 0)                                AS returncode,
        (EXTRACT(EPOCH FROM (finished - last_run)) * 1000)::bigint AS duration_ms,
        CASE WHEN output IS NULL OR output = '' THEN ''
             ELSE left(output, 200) END                        AS message,
        COALESCE(command, '')                                  AS command,
        ''                                                     AS notice,
        ''                                                     AS severity
    FROM timetable.execution_log
    WHERE ($1 = '' OR client_name = $1)
      AND ($2 = 0  OR chain_id = $2)
),
windowed AS (
    SELECT * FROM feed ORDER BY ts DESC LIMIT $3
),
-- run_vxid identifies the chain run. Rows that carry a vxid (every task/exec
-- line and "Chain executed successfully") use their own; the only vxid-less
-- chained line is "Starting chain", which borrows the vxid of the nearest
-- vxid-bearing row at-or-after it in the same chain+client (a lateral lookup).
-- Keying runs by the real vxid — not a positional counter — means rows are
-- never mis-assigned across overlapping runs or ts ties. Chain-less rows keep
-- run_vxid '' and are treated as standalone lines.
runs AS (
    SELECT w.*,
        COALESCE(NULLIF(w.vxid, ''), nx.vxid, '') AS run_vxid
    FROM windowed w
    LEFT JOIN LATERAL (
        SELECT NULLIF(f.vxid, '') AS vxid
        FROM windowed f
        WHERE f.chain_id = w.chain_id
          AND f.client_name = w.client_name
          AND NULLIF(f.vxid, '') IS NOT NULL
          AND f.ts >= w.ts
        ORDER BY f.ts
        LIMIT 1
    ) nx ON w.chain_id <> 0 AND NULLIF(w.vxid, '') IS NULL
),
ranked AS (
    SELECT *,
        -- tree_rank fixes intra-run order on ts ties: the chain start heads the
        -- branch (0) and "Chain executed successfully" always ends it (2);
        -- everything else (1) is ordered purely by timestamp.
        CASE WHEN source='log' AND task_id=0 AND message='Starting chain' THEN 0
             WHEN source='log' AND task_id=0 AND message='Chain executed successfully' THEN 2
             ELSE 1 END AS tree_rank,
        -- A chained run anchors on its run's latest ts so the whole run stays
        -- contiguous and is placed by when the run finished. A chain-less row
        -- anchors on its own ts so it interleaves with runs by time; consecutive
        -- chain-less rows that render adjacently are re-sorted ascending by the
        -- renderer (see renderActivityTree), so within-block order is correct
        -- even though the feed is newest-first.
        CASE WHEN chain_id = 0 THEN ts
             ELSE max(ts) OVER (PARTITION BY chain_id, client_name, run_vxid) END AS anchor_ts,
        -- Only chained runs have a header; chain-less rows are standalone.
        (chain_id <> 0 AND row_number() OVER (
            PARTITION BY chain_id, client_name, run_vxid
            ORDER BY CASE WHEN source='log' AND task_id=0 AND message='Starting chain' THEN 0 ELSE 1 END, ts
        ) = 1) AS is_header
    FROM runs
)
SELECT
    ts_str AS ts, source, client_name, chain_id, chain_name, task_id,
    run_vxid AS vxid, level, returncode, duration_ms, message, command,
    notice, severity, is_header
FROM ranked
ORDER BY
    anchor_ts DESC,                       -- newest run / system line first, interleaved
    chain_id, client_name, run_vxid,      -- keep a run's lines together
    tree_rank, ts`

// ListActivityTree returns the unified feed grouped by chain run for the
// `log list -o tree` view. All grouping/ordering is done in SQL (window
// functions); the renderer only reads is_header to decide header vs. child.
func (c *PgClient) ListActivityTree(ctx context.Context, f LogFilter) ([]ActivityEntry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.pool.Query(ctx, activityTreeSQL, f.ClientName, f.ChainID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[ActivityEntry])
}

// TailActivity streams the unified activity feed, polling both timetable.log
// and timetable.execution_log for new rows using a server-anchored timestamp
// cursor (same technique as TailLogs).
func (c *PgClient) TailActivity(ctx context.Context, f LogFilter, emit func(ActivityEntry)) error {
	const q = `
SELECT
    ts,
    to_char(ts, 'YYYY-MM-DD HH24:MI:SS.MS') AS ts_str,
    'log'                                    AS source,
    COALESCE(client_name, '')                AS client_name,
    COALESCE((message_data->'chain'->>'ChainID')::bigint, 0)         AS chain_id,
    COALESCE(message_data->'chain'->>'ChainName', '')               AS chain_name,
    COALESCE((message_data->'task'->>'TaskID')::bigint, 0)          AS task_id,
    COALESCE(message_data->>'vxid', '')                             AS vxid,
    COALESCE(NULLIF(message_data->>'severity', ''), log_level::text) AS level,
    0                                        AS returncode,
    0::bigint                                AS duration_ms,
    COALESCE(message, '')                    AS message,
    ''                                       AS command,
    COALESCE(message_data->>'notice', '')    AS notice,
    COALESCE(message_data->>'severity', '')  AS severity
FROM timetable.log
WHERE ts > $1
  AND ($2 = '' OR client_name = $2)
  AND ($3 = 0  OR (message_data->'chain'->>'ChainID')::bigint = $3)

UNION ALL

SELECT
    last_run                                               AS ts,
    to_char(last_run, 'YYYY-MM-DD HH24:MI:SS.MS')         AS ts_str,
    'exec'                                                 AS source,
    client_name,
    COALESCE(chain_id, 0)                                  AS chain_id,
    ''                                                     AS chain_name,
    COALESCE(task_id, 0)                                   AS task_id,
    COALESCE(txid::text, '')                               AS vxid,
    CASE WHEN finished IS NULL       THEN 'RUNNING'
         WHEN returncode = 0        THEN 'OK'
         WHEN ignore_error          THEN 'WARN'
         ELSE                            'FAIL'
    END                                                    AS level,
    COALESCE(returncode, 0)                                AS returncode,
    (EXTRACT(EPOCH FROM (finished - last_run)) * 1000)::bigint AS duration_ms,
    CASE
        WHEN output IS NULL OR output = '' THEN ''
        ELSE left(output, 200)
    END                                                    AS message,
    COALESCE(command, '')                                  AS command,
    ''                                                     AS notice,
    ''                                                     AS severity
FROM timetable.execution_log
WHERE last_run > $1
  AND ($2 = '' OR client_name = $2)
  AND ($3 = 0  OR chain_id = $3)

ORDER BY ts ASC`

	// Initialise cursor from the server clock so client/server drift doesn't
	// cause rows to be missed (same pattern as TailLogs).
	var cursor time.Time
	if err := c.pool.QueryRow(ctx, `SELECT clock_timestamp() - interval '1 second'`).Scan(&cursor); err != nil {
		return err
	}

	type rawRow struct {
		Ts         time.Time `db:"ts"`
		TsStr      string    `db:"ts_str"`
		Source     string    `db:"source"`
		ClientName string    `db:"client_name"`
		ChainID    int64     `db:"chain_id"`
		ChainName  string    `db:"chain_name"`
		TaskID     int64     `db:"task_id"`
		Vxid       string    `db:"vxid"`
		Level      string    `db:"level"`
		Returncode int       `db:"returncode"`
		DurationMS int64     `db:"duration_ms"`
		Message    string    `db:"message"`
		Command    string    `db:"command"`
		Notice     string    `db:"notice"`
		Severity   string    `db:"severity"`
	}

	tick := time.NewTicker(tailPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			rows, err := c.pool.Query(ctx, q, cursor, f.ClientName, f.ChainID)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			raws, err := pgx.CollectRows(rows, pgx.RowToStructByName[rawRow])
			if err != nil {
				return err
			}
			for _, r := range raws {
				emit(ActivityEntry{
					TS:         r.TsStr,
					Source:     r.Source,
					ClientName: r.ClientName,
					ChainID:    r.ChainID,
					ChainName:  r.ChainName,
					TaskID:     r.TaskID,
					Vxid:       r.Vxid,
					Level:      r.Level,
					Returncode: r.Returncode,
					DurationMS: r.DurationMS,
					Message:    r.Message,
					Command:    r.Command,
					Notice:     r.Notice,
					Severity:   r.Severity,
				})
				if r.Ts.After(cursor) {
					cursor = r.Ts
				}
			}
		}
	}
}
