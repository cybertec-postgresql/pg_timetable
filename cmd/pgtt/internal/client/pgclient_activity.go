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
const activitySQL = `
SELECT
    to_char(ts, 'YYYY-MM-DD HH24:MI:SS.MS') AS ts,
    'log'                                    AS source,
    COALESCE(client_name, '')                AS client_name,
    0::bigint                                AS chain_id,
    0::bigint                                AS task_id,
    0::bigint                                AS txid,
    log_level::text                          AS level,
    0                                        AS returncode,
    0::bigint                                AS duration_ms,
    COALESCE(message, '')                    AS message,
    ''                                       AS command
FROM timetable.log
WHERE ($1 = '' OR client_name = $1)
  AND ($2 = 0  OR (message_data->'chain'->>'ChainID')::bigint = $2)

UNION ALL

SELECT
    to_char(last_run, 'YYYY-MM-DD HH24:MI:SS.MS')         AS ts,
    'exec'                                                 AS source,
    client_name,
    COALESCE(chain_id, 0)                                  AS chain_id,
    COALESCE(task_id, 0)                                   AS task_id,
    txid,
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
    COALESCE(command, '')                                  AS command
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
    0::bigint                                AS chain_id,
    0::bigint                                AS task_id,
    0::bigint                                AS txid,
    log_level::text                          AS level,
    0                                        AS returncode,
    0::bigint                                AS duration_ms,
    COALESCE(message, '')                    AS message,
    ''                                       AS command
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
    COALESCE(task_id, 0)                                   AS task_id,
    txid,
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
    COALESCE(command, '')                                  AS command
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
		TaskID     int64     `db:"task_id"`
		Txid       int64     `db:"txid"`
		Level      string    `db:"level"`
		Returncode int       `db:"returncode"`
		DurationMS int64     `db:"duration_ms"`
		Message    string    `db:"message"`
		Command    string    `db:"command"`
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
					TaskID:     r.TaskID,
					Txid:       r.Txid,
					Level:      r.Level,
					Returncode: r.Returncode,
					DurationMS: r.DurationMS,
					Message:    r.Message,
					Command:    r.Command,
				})
				if r.Ts.After(cursor) {
					cursor = r.Ts
				}
			}
		}
	}
}
