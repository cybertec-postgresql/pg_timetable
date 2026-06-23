package client_test

// Performance tests (§6 of spec-tool-pgtt-cli.md):
// Validate chain list and log queries remain responsive with ≥500 chains and a
// large execution_log; assert query bounds (LIMIT/pagination).

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	perfChainCount  = 500
	perfExecLogRows = 2000 // 4 runs × 500 chains
	perfMaxListMS   = 5000 // 5 s ceiling — very generous for a cold container
	perfMaxLogsMS   = 2000
)

// seedPerf creates perfChainCount chains and perfExecLogRows execution_log rows.
// Uses COPY for speed to keep setup time manageable in CI.
func seedPerf(t *testing.T, tc *testutils.PostgresTestContainer) {
	t.Helper()
	ctx := context.Background()

	// Bulk-insert chains via a single CTE.
	_, err := tc.Engine.ConfigDb.Exec(ctx, fmt.Sprintf(`
INSERT INTO timetable.chain (chain_name, run_at, live)
SELECT 'perf-chain-' || g, '* * * * *', TRUE
FROM generate_series(1, %d) g
ON CONFLICT DO NOTHING`, perfChainCount))
	require.NoError(t, err)

	// Bulk-insert execution_log rows: 4 finished runs per chain.
	_, err = tc.Engine.ConfigDb.Exec(ctx, fmt.Sprintf(`
INSERT INTO timetable.execution_log
    (chain_id, task_id, txid, last_run, finished, pid, returncode, ignore_error, kind, command, client_name)
SELECT
    c.chain_id,
    0,
    (c.chain_id * 10 + r)::bigint,
    now() - (r || ' minutes')::interval,
    now() - (r || ' minutes')::interval + '1 second'::interval,
    1, 0, FALSE, 'SQL', 'SELECT 1', 'perf-worker'
FROM timetable.chain c
CROSS JOIN generate_series(1, 4) r
WHERE c.chain_name LIKE 'perf-chain-%%'
LIMIT %d`, perfExecLogRows))
	require.NoError(t, err)

	// Bulk-insert log rows.
	_, err = tc.Engine.ConfigDb.Exec(ctx, fmt.Sprintf(`
INSERT INTO timetable.log (pid, log_level, client_name, message)
SELECT 1, 'INFO', 'perf-worker', 'perf test message'
FROM generate_series(1, %d)`, perfExecLogRows))
	require.NoError(t, err)
}

// TestListChains_Performance verifies chain list completes within perfMaxListMS
// even with ≥500 chains and a large execution_log (spec §6).
func TestListChains_Performance(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	seedPerf(t, tc)

	c := newConnectedClient(t, tc)
	start := time.Now()
	chains, err := c.ListChains(context.Background())
	elapsed := time.Since(start).Milliseconds()

	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(chains), perfChainCount,
		"should return at least %d chains", perfChainCount)
	assert.LessOrEqual(t, elapsed, int64(perfMaxListMS),
		"ListChains took %dms, expected ≤%dms", elapsed, perfMaxListMS)
	t.Logf("ListChains(%d chains): %dms", len(chains), elapsed)
}

// TestListLogs_Performance verifies log list respects its LIMIT and returns
// within perfMaxLogsMS (spec §6, REQ-012 pagination bounds).
func TestListLogs_Performance(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	seedPerf(t, tc)

	c := newConnectedClient(t, tc)
	start := time.Now()
	logs, err := c.ListLogs(context.Background(), client.LogFilter{Limit: 100})
	elapsed := time.Since(start).Milliseconds()

	require.NoError(t, err)
	assert.LessOrEqual(t, len(logs), 100, "LIMIT must be respected")
	assert.LessOrEqual(t, elapsed, int64(perfMaxLogsMS),
		"ListLogs took %dms, expected ≤%dms", elapsed, perfMaxLogsMS)
	t.Logf("ListLogs(limit=100 from %d rows): %dms", perfExecLogRows, elapsed)
}

// TestListRuns_Performance verifies chain runs query is bounded by its limit.
func TestListRuns_Performance(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	seedPerf(t, tc)

	c := newConnectedClient(t, tc)
	start := time.Now()
	runs, err := c.ListRuns(context.Background(), "perf-chain-1", 10)
	elapsed := time.Since(start).Milliseconds()

	require.NoError(t, err)
	assert.LessOrEqual(t, len(runs), 10, "limit must be respected")
	assert.LessOrEqual(t, elapsed, int64(perfMaxListMS),
		"ListRuns took %dms", elapsed)
	t.Logf("ListRuns(limit=10): %dms, got %d rows", elapsed, len(runs))
}
