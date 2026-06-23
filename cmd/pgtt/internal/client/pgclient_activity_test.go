package client_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListActivity_MergesBothSources verifies the unified feed contains rows
// from both timetable.log and timetable.execution_log.
func TestListActivity_MergesBothSources(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	chainID := seedJob(t, tc, "activity-chain", "* * * * *")

	// Insert a scheduler log row.
	_, err := tc.Engine.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.log (pid, log_level, client_name, message)
		 VALUES (1, 'INFO', 'act-worker', 'chain dispatched')`)
	require.NoError(t, err)

	// Insert an execution_log row (simulates a completed task).
	seedExecLog(t, tc, chainID, 0, 5001, 0, "SELECT done", true)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivity(ctx, client.LogFilter{Limit: 50})
	require.NoError(t, err)

	sources := map[string]bool{}
	for _, e := range entries {
		sources[e.Source] = true
	}
	assert.True(t, sources["log"], "should contain timetable.log rows")
	assert.True(t, sources["exec"], "should contain timetable.execution_log rows")
}

// TestListActivity_ExecRows verifies execution_log rows carry chain_id, level,
// returncode, and message (truncated output).
func TestListActivity_ExecRows(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	chainID := seedJob(t, tc, "exec-act-chain", "* * * * *")
	seedExecLog(t, tc, chainID, 0, 6001, 1, "error output", true) // failed

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivity(ctx, client.LogFilter{ChainID: chainID, Limit: 10})
	require.NoError(t, err)

	var execEntry *client.ActivityEntry
	for i := range entries {
		if entries[i].Source == "exec" {
			execEntry = &entries[i]
			break
		}
	}
	require.NotNil(t, execEntry, "expected an exec row")
	assert.Equal(t, int64(chainID), execEntry.ChainID)
	assert.Equal(t, "FAIL", execEntry.Level)
	assert.Equal(t, 1, execEntry.Returncode)
	assert.Contains(t, execEntry.Message, "error output")
}

// TestListActivity_FilterByChain verifies chain filter restricts execution_log rows.
func TestListActivity_FilterByChain(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	id1 := seedJob(t, tc, "filt-chain-1", "* * * * *")
	id2 := seedJob(t, tc, "filt-chain-2", "* * * * *")
	seedExecLog(t, tc, id1, 0, 7001, 0, "", true)
	seedExecLog(t, tc, id2, 0, 7002, 0, "", true)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivity(ctx, client.LogFilter{ChainID: id1, Limit: 20})
	require.NoError(t, err)

	for _, e := range entries {
		if e.Source == "exec" {
			assert.Equal(t, int64(id1), e.ChainID,
				"exec rows must only belong to chain %d", id1)
		}
	}
}

// TestTailActivity_ReceivesExecRows verifies TailActivity picks up execution_log
// rows inserted after it starts.
func TestTailActivity_ReceivesExecRows(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	chainID := seedJob(t, tc, "tail-act-chain", "* * * * *")

	// Create client BEFORE starting the goroutine so TailActivity's cursor
	// (initialised from clock_timestamp()) is set before any rows are inserted.
	cl := newConnectedClient(t, tc)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var mu sync.Mutex
	var received []client.ActivityEntry

	done := make(chan error, 1)
	go func() {
		done <- cl.TailActivity(ctx, client.LogFilter{}, func(e client.ActivityEntry) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		})
	}()

	// Give the first tick time to fire, then insert an execution_log row.
	time.Sleep(1500 * time.Millisecond)
	seedExecLog(t, tc, chainID, 0, 8001, 0, "tail output", true)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	var found bool
	for _, e := range received {
		if e.Source == "exec" && e.ChainID == int64(chainID) {
			found = true
			assert.Equal(t, "OK", e.Level)
			assert.Contains(t, e.Message, "tail output")
		}
	}
	assert.True(t, found, "should have received execution_log row for the seeded chain")
}

// TestListActivity_LogRowContextFromMessageData verifies that timetable.log rows
// surface chain id+name, task id, and vxid mined from message_data (P7-1/P7-2),
// instead of the previous contextless zeros. The message_data shape mirrors what
// the scheduler writes: a "chain" object (ChainID/ChainName), a "task" object
// (TaskID/TaskName) and a top-level "vxid".
func TestListActivity_LogRowContextFromMessageData(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	_, err := tc.Engine.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.log (pid, log_level, client_name, message, message_data)
		 VALUES (1, 'INFO', 'demo_worker', 'Starting task',
		         '{"chain":{"ChainID":1,"ChainName":"notify_every_minute"},
		           "task":{"TaskID":1,"TaskName":"notify"},
		           "vxid":"21474836598"}'::jsonb)`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivity(ctx, client.LogFilter{ClientName: "demo_worker", Limit: 10})
	require.NoError(t, err)

	var logEntry *client.ActivityEntry
	for i := range entries {
		if entries[i].Source == "log" {
			logEntry = &entries[i]
			break
		}
	}
	require.NotNil(t, logEntry, "expected a log row")
	assert.Equal(t, int64(1), logEntry.ChainID)
	assert.Equal(t, "notify_every_minute", logEntry.ChainName)
	assert.Equal(t, int64(1), logEntry.TaskID)
	assert.Equal(t, "21474836598", logEntry.Vxid)
	assert.Equal(t, "Starting task", logEntry.Message)
}

// TestListActivity_LogRowWithoutContext verifies a plain timetable.log row (no
// message_data) still renders cleanly: empty chain name / vxid and zero ids,
// rather than failing the jsonb extraction.
func TestListActivity_LogRowWithoutContext(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	_, err := tc.Engine.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.log (pid, log_level, client_name, message)
		 VALUES (1, 'INFO', 'ctx-worker', 'Retrieve scheduled chains to run')`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivity(ctx, client.LogFilter{ClientName: "ctx-worker", Limit: 10})
	require.NoError(t, err)

	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, "log", e.Source)
	assert.Equal(t, int64(0), e.ChainID)
	assert.Empty(t, e.ChainName)
	assert.Equal(t, int64(0), e.TaskID)
	assert.Empty(t, e.Vxid)
}

// TestListActivity_FilterByChainMatchesLogRows verifies the chain filter now
// matches timetable.log rows via message_data (not just execution_log).
func TestListActivity_FilterByChainMatchesLogRows(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	_, err := tc.Engine.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.log (pid, log_level, client_name, message, message_data)
		 VALUES (1, 'INFO', 'filt-worker', 'chain 1 msg',
		         '{"chain":{"ChainID":1,"ChainName":"one"}}'::jsonb),
		        (1, 'INFO', 'filt-worker', 'chain 2 msg',
		         '{"chain":{"ChainID":2,"ChainName":"two"}}'::jsonb)`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivity(ctx, client.LogFilter{ChainID: 1, Limit: 10})
	require.NoError(t, err)

	require.NotEmpty(t, entries)
	for _, e := range entries {
		assert.Equal(t, int64(1), e.ChainID, "filter must restrict log rows to chain 1")
	}
}

// TestListActivity_NoticeSeverity verifies that a PostgreSQL NOTICE captured by
// the scheduler (message_data with "notice"/"severity") surfaces those fields and
// that severity drives the row's level instead of the bare log level (P7-3).
func TestListActivity_NoticeSeverity(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	_, err := tc.Engine.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.log (pid, log_level, client_name, message, message_data)
		 VALUES (1, 'INFO', 'demo_worker', 'Notice received',
		         '{"notice":"Message by demo_worker from chain 2: \"Hey from client messages task\"",
		           "severity":"NOTICE"}'::jsonb)`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivity(ctx, client.LogFilter{ClientName: "demo_worker", Limit: 10})
	require.NoError(t, err)

	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, "log", e.Source)
	assert.Equal(t, "NOTICE", e.Severity, "severity surfaced from message_data")
	assert.Contains(t, e.Notice, "Hey from client messages task", "notice text surfaced")
	assert.Equal(t, "NOTICE", e.Level, "severity must drive level, not the bare log_level INFO")
}

// TestListActivity_SeverityFallsBackToLogLevel verifies that ordinary log rows
// (no severity in message_data) keep their log_level as the level.
func TestListActivity_SeverityFallsBackToLogLevel(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	_, err := tc.Engine.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.log (pid, log_level, client_name, message)
		 VALUES (1, 'ERROR', 'sev-worker', 'something broke')`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivity(ctx, client.LogFilter{ClientName: "sev-worker", Limit: 10})
	require.NoError(t, err)

	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, "ERROR", e.Level)
	assert.Empty(t, e.Severity)
	assert.Empty(t, e.Notice)
}

// TestListActivityTree_GroupsRunsInSQL verifies the SQL-side run grouping:
// two runs of the same chain (distinct vxids) are returned as separate,
// contiguous branches headed by their "Starting chain" line with the run vxid
// broadcast onto it; a chain-less scheduler row interleaves by its own
// timestamp (between the runs) rather than being dumped at the end.
func TestListActivityTree_GroupsRunsInSQL(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	// Two runs of chain 1 (vxids 100 then 200) plus a chain-less scheduler row
	// whose ts (10:01:00.050) falls between the two runs, inserted out of order
	// to prove SQL does the grouping/ordering.
	_, err := tc.Engine.ConfigDb.Exec(ctx, `
		INSERT INTO timetable.log (ts, pid, log_level, client_name, message, message_data) VALUES
		-- run 1 (older)
		('2026-06-23 10:00:00.100', 1, 'INFO', 'w', 'Starting chain',
		 '{"chain":{"ChainID":1,"ChainName":"one"}}'),
		('2026-06-23 10:00:00.120', 1, 'INFO', 'w', 'Starting task',
		 '{"chain":{"ChainID":1,"ChainName":"one"},"task":{"TaskID":1},"vxid":"100"}'),
		('2026-06-23 10:00:00.130', 1, 'INFO', 'w', 'Chain executed successfully',
		 '{"chain":{"ChainID":1,"ChainName":"one"},"vxid":"100"}'),
		-- run 2 (newer)
		('2026-06-23 10:01:00.100', 1, 'INFO', 'w', 'Starting chain',
		 '{"chain":{"ChainID":1,"ChainName":"one"}}'),
		('2026-06-23 10:01:00.120', 1, 'INFO', 'w', 'Starting task',
		 '{"chain":{"ChainID":1,"ChainName":"one"},"task":{"TaskID":1},"vxid":"200"}'),
		-- chain-less scheduler row, between the two runs by time
		('2026-06-23 10:01:00.050', 1, 'INFO', 'w', 'Retrieve scheduled chains to run', '{}')
	`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivityTree(ctx, client.LogFilter{ClientName: "w", Limit: 100})
	require.NoError(t, err)
	require.Len(t, entries, 6)

	// Newest run (vxid 200, anchor 10:01:00.120) first, headed by Starting chain.
	assert.True(t, entries[0].IsHeader)
	assert.Equal(t, "Starting chain", entries[0].Message)
	assert.Equal(t, "200", entries[0].Vxid, "run vxid broadcast onto the vxid-less Starting chain")
	assert.Equal(t, int64(1), entries[0].ChainID)
	assert.Equal(t, "200", entries[1].Vxid)
	assert.False(t, entries[1].IsHeader)

	// The chain-less row interleaves next (its ts 10:01:00.050 is newer than the
	// older run's anchor 10:00:00.130 but older than run 2's anchor).
	assert.Equal(t, int64(0), entries[2].ChainID)
	assert.Equal(t, "Retrieve scheduled chains to run", entries[2].Message)
	assert.False(t, entries[2].IsHeader, "chain-less rows are standalone, never headers")

	// Then the older run (vxid 100), headed by its own Starting chain.
	assert.True(t, entries[3].IsHeader)
	assert.Equal(t, "Starting chain", entries[3].Message)
	for _, e := range entries[3:6] {
		assert.Equal(t, "100", e.Vxid, "second branch must be the other run, never merged")
	}
}

// TestListActivityTree_SystemRowsAreStandalone verifies the SQL contract for
// chain-less ("system") rows: they are returned interleaved by their own ts
// (newest-first, like the rest of the feed) and never marked as headers.
// Within-block ascending ordering for display is the renderer's job
// (see TestRenderActivityTree_SystemLinesInterleave), not the query's.
func TestListActivityTree_SystemRowsAreStandalone(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	_, err := tc.Engine.ConfigDb.Exec(ctx, `
		INSERT INTO timetable.log (ts, pid, log_level, client_name, message, message_data) VALUES
		('2026-06-23 12:00:00.300', 1, 'INFO', 'w', 'Notice received', '{}'),
		('2026-06-23 12:00:00.200', 1, 'INFO', 'w', 'Retrieve interval chains to run', '{}'),
		('2026-06-23 12:00:00.100', 1, 'INFO', 'w', 'Retrieve scheduled chains to run', '{}')
	`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivityTree(ctx, client.LogFilter{ClientName: "w", Limit: 100})
	require.NoError(t, err)
	require.Len(t, entries, 3)

	for _, e := range entries {
		assert.Equal(t, int64(0), e.ChainID)
		assert.False(t, e.IsHeader, "chain-less rows are standalone, never headers")
	}
	// Newest-first by ts (the renderer re-sorts the contiguous block ascending).
	assert.Equal(t, "Notice received", entries[0].Message)                  // .300
	assert.Equal(t, "Retrieve interval chains to run", entries[1].Message)  // .200
	assert.Equal(t, "Retrieve scheduled chains to run", entries[2].Message) // .100
}

// TestListActivityTree_FilterByChain verifies the chain filter applies to the
// tree query (both sources) the same way it does for the flat feed.
func TestListActivityTree_FilterByChain(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	_, err := tc.Engine.ConfigDb.Exec(ctx, `
		INSERT INTO timetable.log (pid, log_level, client_name, message, message_data) VALUES
		(1, 'INFO', 'w', 'Starting chain', '{"chain":{"ChainID":1,"ChainName":"one"}}'),
		(1, 'INFO', 'w', 'Starting chain', '{"chain":{"ChainID":2,"ChainName":"two"}}')
	`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	entries, err := c.ListActivityTree(ctx, client.LogFilter{ChainID: 1, Limit: 100})
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		assert.Equal(t, int64(1), e.ChainID)
	}
}
