package client_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedJob inserts a one-task chain via timetable.add_job and returns its id.
func seedJob(t *testing.T, tc *testutils.PostgresTestContainer, name, schedule string) int {
	t.Helper()
	var id int
	err := tc.Engine.ConfigDb.QueryRow(context.Background(),
		`SELECT timetable.add_job($1, $2, 'SELECT 1')`, name, schedule,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func newConnectedClient(t *testing.T, tc *testutils.PostgresTestContainer) client.Client {
	t.Helper()
	c := client.New(currentSchema)
	require.NoError(t, c.Connect(context.Background(), tc.ConnStr))
	t.Cleanup(c.Close)
	return c
}

// TestListChains verifies all seeded chains are returned (REQ-002 / AC-001).
func TestListChains(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	seedJob(t, tc, "chain-a", "* * * * *")
	seedJob(t, tc, "chain-b", "0 0 * * *")

	c := newConnectedClient(t, tc)
	chains, err := c.ListChains(context.Background())
	require.NoError(t, err)
	require.Len(t, chains, 2)

	names := map[string]bool{}
	for _, ch := range chains {
		names[ch.ChainName] = true
		assert.NotZero(t, ch.ChainID)
	}
	assert.True(t, names["chain-a"])
	assert.True(t, names["chain-b"])
}

// TestShowChain_ByIDAndName verifies a chain and its tasks are returned (REQ-003).
func TestShowChain_ByIDAndName(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "chain-show", "* * * * *")
	c := newConnectedClient(t, tc)

	byID, tasks, err := c.ShowChain(context.Background(), strconv.Itoa(id))
	require.NoError(t, err)
	assert.Equal(t, "chain-show", byID.ChainName)
	require.Len(t, tasks, 1)
	assert.Equal(t, "SELECT 1", tasks[0].Command)

	byName, _, err := c.ShowChain(context.Background(), "chain-show")
	require.NoError(t, err)
	assert.Equal(t, id, byName.ChainID)
}

// TestShowChain_NotFound verifies a missing chain produces a clear error.
func TestShowChain_NotFound(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	c := newConnectedClient(t, tc)
	_, _, err := c.ShowChain(context.Background(), "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestListSessions_Empty verifies the sessions query works on a fresh DB.
func TestListSessions_Empty(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	c := newConnectedClient(t, tc)
	sessions, err := c.ListSessions(context.Background())
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

// TestListActiveChains_Empty verifies the active-chains query works.
func TestListActiveChains_Empty(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	c := newConnectedClient(t, tc)
	active, err := c.ListActiveChains(context.Background())
	require.NoError(t, err)
	assert.Empty(t, active)
}

// TestListLogs verifies log filtering and limit work (REQ-012).
func TestListLogs(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	for range 3 {
		_, err := tc.Engine.ConfigDb.Exec(ctx,
			`INSERT INTO timetable.log (pid, log_level, client_name, message)
			 VALUES (1, 'INFO', 'worker1', 'hello')`)
		require.NoError(t, err)
	}

	c := newConnectedClient(t, tc)

	all, err := c.ListLogs(ctx, client.LogFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)

	limited, err := c.ListLogs(ctx, client.LogFilter{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, limited, 2)

	byClient, err := c.ListLogs(ctx, client.LogFilter{ClientName: "worker1"})
	require.NoError(t, err)
	assert.Len(t, byClient, 3)

	none, err := c.ListLogs(ctx, client.LogFilter{ClientName: "nobody"})
	require.NoError(t, err)
	assert.Empty(t, none)
}
