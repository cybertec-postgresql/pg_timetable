package client

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// tailPollInterval is how often TailLogs polls for new rows.
const tailPollInterval = time.Second

// TailLogs streams new timetable.log entries to emit as they arrive, using a
// polling cursor on the ts column (REQ-013 / P5-1).
//
// Why polling rather than LISTEN/NOTIFY: the scheduler writes log rows via
// CopyFrom/INSERT directly to timetable.log without publishing a NOTIFY.
// There is no existing log-notification channel in pgengine to hook into.
// A 1-second poll is low-overhead and matches the UX expectation of a log tail.
//
// Graceful shutdown: returns nil when ctx is cancelled (P5-2).
func (c *PgClient) TailLogs(ctx context.Context, f LogFilter, emit func(LogEntry)) error {
	// Fetch rows newer than cursor, returning the raw ts for cursor advancement.
	// Cursor is a timestamptz passed as $1; the DB compares it against ts directly
	// so timezone differences between client and server are not an issue.
	const q = `
SELECT ts,
       to_char(ts, 'YYYY-MM-DD HH24:MI:SS') AS ts_str,
       pid,
       log_level::text                       AS log_level,
       COALESCE(client_name, '')             AS client_name,
       COALESCE(message, '')                 AS message
FROM timetable.log
WHERE ts > $1
  AND ($2 = '' OR client_name = $2)
  AND ($3 = 0  OR (message_data->'chain'->>'ChainID')::bigint = $3)
ORDER BY ts ASC`

	// Initialise the cursor from the database clock (not the client clock) to
	// avoid missing rows when the client and server clocks differ.
	var cursor time.Time
	if err := c.pool.QueryRow(ctx, `SELECT clock_timestamp() - interval '1 second'`).Scan(&cursor); err != nil {
		return err
	}

	tick := time.NewTicker(tailPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			type rawRow struct {
				Ts         time.Time `db:"ts"`
				TsStr      string    `db:"ts_str"`
				PID        int       `db:"pid"`
				LogLevel   string    `db:"log_level"`
				ClientName string    `db:"client_name"`
				Message    string    `db:"message"`
			}
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
				emit(LogEntry{
					TS:         r.TsStr,
					PID:        r.PID,
					LogLevel:   r.LogLevel,
					ClientName: r.ClientName,
					Message:    r.Message,
				})
				// Advance cursor to the latest ts seen — anchored to the
				// server clock, so no client/server drift problem.
				if r.Ts.After(cursor) {
					cursor = r.Ts
				}
			}
			// If no rows arrived, do NOT advance the cursor — wait for the
			// server to produce rows newer than what we last saw.
		}
	}
}
