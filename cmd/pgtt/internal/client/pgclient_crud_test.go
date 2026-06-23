package client_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

// TestCreateEditDeleteChain covers the full chain lifecycle (REQ-004).
func TestCreateEditDeleteChain(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	c := newConnectedClient(t, tc)
	ctx := context.Background()

	id, err := c.CreateChain(ctx, client.ChainSpec{
		Name:     "crud-chain",
		Schedule: "* * * * *",
		Command:  "SELECT 1",
		Live:     true,
	})
	require.NoError(t, err)
	assert.Positive(t, id)

	// Edit: change schedule.
	require.NoError(t, c.EditChain(ctx, "crud-chain", client.ChainEdit{
		Schedule: ptr("0 0 * * *"),
		Live:     ptr(false),
	}))
	chains, err := c.ListChains(ctx)
	require.NoError(t, err)
	var found client.ChainListItem
	for _, ch := range chains {
		if ch.ChainName == "crud-chain" {
			found = ch
		}
	}
	assert.Equal(t, "0 0 * * *", found.RunAt)
	assert.False(t, found.Live)

	// EditChain with nothing to change returns an error.
	assert.Error(t, c.EditChain(ctx, "crud-chain", client.ChainEdit{}))

	// Delete.
	require.NoError(t, c.DeleteChain(ctx, "crud-chain"))
	chains, err = c.ListChains(ctx)
	require.NoError(t, err)
	for _, ch := range chains {
		assert.NotEqual(t, "crud-chain", ch.ChainName)
	}
}

// TestDeleteChain_NotFound verifies a clear error for unknown chains.
func TestDeleteChain_NotFound(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	c := newConnectedClient(t, tc)
	assert.Error(t, c.DeleteChain(context.Background(), "ghost"))
}

// TestAddEditDeleteTask covers the task lifecycle within a chain (REQ-004).
func TestAddEditDeleteTask(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	c := newConnectedClient(t, tc)
	ctx := context.Background()

	chainID, err := c.CreateChain(ctx, client.ChainSpec{
		Name:     "task-chain",
		Schedule: "* * * * *",
		Command:  "SELECT 1",
	})
	require.NoError(t, err)

	taskID, err := c.AddTask(ctx, strconv.Itoa(chainID), client.TaskSpec{
		Command: "SELECT 2",
		Kind:    "SQL",
	})
	require.NoError(t, err)
	assert.Positive(t, taskID)

	// Verify it shows up in ShowChain.
	_, tasks, err := c.ShowChain(ctx, "task-chain")
	require.NoError(t, err)
	assert.Len(t, tasks, 2)

	// Edit the task.
	newCmd := "SELECT 99"
	require.NoError(t, c.EditTask(ctx, taskID, client.TaskEdit{Command: &newCmd}))
	_, tasks, err = c.ShowChain(ctx, "task-chain")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 99", tasks[1].Command)

	// Delete the task.
	require.NoError(t, c.DeleteTask(ctx, taskID))
	_, tasks, err = c.ShowChain(ctx, "task-chain")
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
}

// TestMoveTask verifies task reordering (REQ-008).
func TestMoveTask(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	c := newConnectedClient(t, tc)
	ctx := context.Background()

	chainID, err := c.CreateChain(ctx, client.ChainSpec{
		Name: "move-chain", Schedule: "* * * * *", Command: "SELECT 1",
	})
	require.NoError(t, err)

	t2ID, err := c.AddTask(ctx, strconv.Itoa(chainID), client.TaskSpec{Command: "SELECT 2", Kind: "SQL"})
	require.NoError(t, err)

	// Move task 2 up — it should now precede task 1.
	require.NoError(t, c.MoveTask(ctx, t2ID, true))
	_, tasks, err := c.ShowChain(ctx, "move-chain")
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.Equal(t, "SELECT 2", tasks[0].Command)
	assert.Equal(t, "SELECT 1", tasks[1].Command)
}

// staticYAML is a minimal self-contained chain YAML for round-trip testing.
const staticYAML = `chains:
  - name: rt-chain
    schedule: "0 3 * * *"
    live: true
    tasks:
      - name: noop
        kind: SQL
        command: SELECT 1
`

// TestApplyExportRoundTrip verifies a static chain survives export→apply (AC-006).
func TestApplyExportRoundTrip(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	c := newConnectedClient(t, tc)
	ctx := context.Background()

	// Write the static YAML to a temp file and apply it.
	inFile := t.TempDir() + "/static.yaml"
	require.NoError(t, os.WriteFile(inFile, []byte(staticYAML), 0o644))
	n, err := c.ApplyYAML(ctx, inFile, false)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Export the chain by name.
	data, warnings, err := c.ExportYAML(ctx, []string{"rt-chain"})
	require.NoError(t, err)
	assert.NotEmpty(t, warnings)                // snapshot warning always present
	assert.Contains(t, string(data), "WARNING") // header present (REQ-010)

	// Re-apply with --replace to complete the round-trip (AC-006).
	outFile := t.TempDir() + "/round-trip.yaml"
	require.NoError(t, os.WriteFile(outFile, data, 0o644))
	n2, err := c.ApplyYAML(ctx, outFile, true)
	require.NoError(t, err)
	assert.Equal(t, 1, n2)

	// Verify the chain still exists with the right schedule.
	chains, err := c.ListChains(ctx)
	require.NoError(t, err)
	var found bool
	for _, ch := range chains {
		if ch.ChainName == "rt-chain" {
			found = true
			assert.Equal(t, "0 3 * * *", ch.RunAt)
		}
	}
	assert.True(t, found, "rt-chain should exist after round-trip")
}
