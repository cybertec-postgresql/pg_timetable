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
	const q = `
SELECT to_char(ts, 'YYYY-MM-DD HH24:MI:SS') AS ts,
       pid,
       log_level::text                       AS log_level,
       COALESCE(client_name, '')             AS client_name,
       COALESCE(message, '')                 AS message
FROM timetable.log
WHERE ts > $1
  AND ($2 = '' OR client_name = $2)
  AND ($3 = 0  OR (message_data->>'chain')::bigint = $3)
ORDER BY ts ASC`

	// cursor starts slightly before "now" to catch rows written in the last
	// second before the user ran the command.
	cursor := time.Now().UTC().Add(-tailPollInterval)

	tick := time.NewTicker(tailPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case t := <-tick.C:
			rows, err := c.pool.Query(ctx, q, cursor, f.ClientName, f.ChainID)
			if err != nil {
				if ctx.Err() != nil {
					return nil // cancelled during query
				}
				return err
			}
			entries, err := pgx.CollectRows(rows, pgx.RowToStructByName[LogEntry])
			if err != nil {
				return err
			}
			for _, e := range entries {
				emit(e)
			}
			// Always advance the cursor to the tick time so we only ask for
			// rows newer than the last poll regardless of whether any arrived.
			cursor = t.UTC()
		}
	}
}
