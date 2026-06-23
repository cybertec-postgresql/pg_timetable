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
