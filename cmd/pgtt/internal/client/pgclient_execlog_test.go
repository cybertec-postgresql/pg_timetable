package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedExecLog inserts synthetic execution_log rows for a chain so we can test
// the runs/run-detail queries without a live scheduler.
func seedExecLog(t *testing.T, tc *testutils.PostgresTestContainer,
	chainID, taskID int, txid int64, rc int, output string, finished bool,
) {
	t.Helper()
	ctx := context.Background()
	started := time.Now().UTC().Add(-3 * time.Second)
	var fin *time.Time
	if finished {
		f := started.Add(2 * time.Second)
		fin = &f
	}
	_, err := tc.Engine.ConfigDb.Exec(ctx, `
INSERT INTO timetable.execution_log
    (chain_id, task_id, txid, last_run, finished, pid, returncode, ignore_error, kind, command, output, client_name, params)
VALUES ($1, $2, $3, $4, $5, 1, $6, FALSE, 'SQL', 'SELECT 1', $7, 'test-worker', NULL)`,
		chainID, taskID, txid, started, fin, rc, output)
	require.NoError(t, err)
}

// TestListChains_EnrichedLastRun verifies the new last-run fields are populated
// in chain list (P5-3 / REQ-012).
func TestListChains_EnrichedLastRun(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "enrich-chain", "* * * * *")
	seedExecLog(t, tc, id, 0, 9001, 0, "done", true)

	c := newConnectedClient(t, tc)
	chains, err := c.ListChains(context.Background())
	require.NoError(t, err)

	var found bool
	for _, ch := range chains {
		if ch.ChainName == "enrich-chain" {
			found = true
			assert.Equal(t, "success", ch.LastStatus)
			assert.Equal(t, 0, ch.LastReturncode)
			assert.Equal(t, "test-worker", ch.LastWorker)
			assert.NotEmpty(t, ch.LastRun)
			assert.Positive(t, ch.LastDurationMS)
		}
	}
	assert.True(t, found)
}

// TestListRuns verifies ListRuns returns one row per txid, grouped correctly (P5-4).
func TestListRuns(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "runs-chain", "* * * * *")
	// Two distinct runs (txid 1001, 1002), one task each.
	seedExecLog(t, tc, id, 0, 1001, 0, "", true)
	seedExecLog(t, tc, id, 0, 1002, 1, "", true) // failed run

	c := newConnectedClient(t, tc)
	runs, err := c.ListRuns(context.Background(), "runs-chain", 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)

	// Most recent first (ORDER BY MIN(last_run) DESC).
	assert.Equal(t, int64(1002), runs[0].Txid)
	assert.Equal(t, "failed", runs[0].Status)
	assert.Equal(t, int64(1001), runs[1].Txid)
	assert.Equal(t, "success", runs[1].Status)
}

// TestListRuns_LimitRespected verifies the limit flag is honoured.
func TestListRuns_LimitRespected(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "limit-chain", "* * * * *")
	for txid := range 5 {
		seedExecLog(t, tc, id, 0, int64(2000+txid), 0, "", true)
	}

	c := newConnectedClient(t, tc)
	runs, err := c.ListRuns(context.Background(), "limit-chain", 3)
	require.NoError(t, err)
	assert.Len(t, runs, 3)
}

// TestShowRun verifies ShowRun returns per-task rows for a txid (P5-5).
func TestShowRun(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "detail-chain", "* * * * *")
	// Get the task_id of the task just created.
	var taskID int
	require.NoError(t, tc.Engine.ConfigDb.QueryRow(context.Background(),
		`SELECT task_id FROM timetable.task WHERE chain_id = $1`, id).Scan(&taskID))

	seedExecLog(t, tc, id, taskID, 3001, 0, "hello output", true)

	c := newConnectedClient(t, tc)
	tasks, err := c.ShowRun(context.Background(), 3001)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, int64(taskID), tasks[0].TaskID)
	assert.Equal(t, "SQL", tasks[0].Kind)
	assert.Equal(t, 0, tasks[0].Returncode)
	assert.Equal(t, "hello output", tasks[0].Output)
	assert.NotEmpty(t, tasks[0].StartedAt)
	assert.NotEmpty(t, tasks[0].FinishedAt)
	assert.Positive(t, tasks[0].DurationMS)
}

// TestShowRun_NotFound verifies an empty result for an unknown txid.
func TestShowRun_NotFound(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	c := newConnectedClient(t, tc)
	tasks, err := c.ShowRun(context.Background(), 999999)
	require.NoError(t, err)
	assert.Empty(t, tasks)
}
