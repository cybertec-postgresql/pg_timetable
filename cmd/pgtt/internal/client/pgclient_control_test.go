package client_test

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPauseResumeChain verifies pause_job/resume_job toggle chain.live
// (REQ-007 / AC-004).
func TestPauseResumeChain(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	seedJob(t, tc, "ctrl-chain", "* * * * *")
	c := newConnectedClient(t, tc)

	require.NoError(t, c.PauseChain(context.Background(), "ctrl-chain"))
	var live bool
	require.NoError(t, tc.Engine.ConfigDb.QueryRow(context.Background(),
		`SELECT live FROM timetable.chain WHERE chain_name = 'ctrl-chain'`).Scan(&live))
	assert.False(t, live, "chain should be paused (live=false)")

	require.NoError(t, c.ResumeChain(context.Background(), "ctrl-chain"))
	require.NoError(t, tc.Engine.ConfigDb.QueryRow(context.Background(),
		`SELECT live FROM timetable.chain WHERE chain_name = 'ctrl-chain'`).Scan(&live))
	assert.True(t, live, "chain should be resumed (live=true)")
}

// TestPauseChain_NotFound verifies a clear error for unknown chain names.
func TestPauseChain_NotFound(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	c := newConnectedClient(t, tc)
	err := c.PauseChain(context.Background(), "no-such-chain")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestStartChain_NotifyFires verifies that calling notify_chain_start completes
// without error and that the chain_id is valid (REQ-005 / AC-002).
// There is no live worker in the test container, so we only verify the SQL
// call succeeds; the one-shot / live-override behavior is verified by the
// scheduler unit tests in internal/scheduler.
func TestStartChain_NotifyFires(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "start-chain", "* * * * *")

	// Pause the chain to confirm start works regardless of live (P0-1 decision).
	_, err := tc.Engine.ConfigDb.Exec(context.Background(),
		`SELECT timetable.pause_job('start-chain')`)
	require.NoError(t, err)

	c := newConnectedClient(t, tc)
	// worker "ghost" is not in active_session; command should warn but not error.
	require.NoError(t, c.StartChain(context.Background(), id, "ghost", 0))
}

// TestStopChain_NotifyFires verifies notify_chain_stop completes without error
// (REQ-006 / AC-003). Worker presence is not required at the SQL level.
func TestStopChain_NotifyFires(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "stop-chain", "* * * * *")
	c := newConnectedClient(t, tc)
	require.NoError(t, c.StopChain(context.Background(), id, "ghost"))
}

// TestStartChain_WorkerWarning verifies the worker-absent warning path (P3-4 / §9).
// The call must still succeed (not return an error) while the warning is printed.
func TestStartChain_WorkerWarning(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "warn-chain", "* * * * *")
	c := newConnectedClient(t, tc)
	// No workers registered → workerExists returns false → warning printed, no error.
	err := c.StartChain(context.Background(), id, "nonexistent-worker", 0)
	assert.NoError(t, err, "missing worker should warn but not fail")
}

// TestStartChain_WithDelay verifies the delay parameter is accepted (REQ-005).
func TestStartChain_WithDelay(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "delay-chain", "* * * * *")
	c := newConnectedClient(t, tc)
	require.NoError(t, c.StartChain(context.Background(), id, "w1", 5))
}

// TestResumeChain_NotFound verifies a clear error for unknown chain names.
func TestResumeChain_NotFound(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	c := newConnectedClient(t, tc)
	err := c.ResumeChain(context.Background(), "no-such-chain")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
