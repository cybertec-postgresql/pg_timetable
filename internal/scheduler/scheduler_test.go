package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/otel"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
)

func TestExecuteChainCancelledContextCleansUp(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	pge := container.Engine
	ctx := context.Background()
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())

	var chainID int
	err := pge.ConfigDb.QueryRow(ctx, `
		INSERT INTO timetable.chain(chain_name, max_instances)
		VALUES ('cancel_cleanup_test', 16) RETURNING chain_id`).Scan(&chainID)
	require.NoError(t, err)

	_, err = pge.ConfigDb.Exec(ctx, `
		INSERT INTO timetable.task(chain_id, task_order, kind, command)
		VALUES ($1, 1, 'SQL', 'SELECT pg_sleep(0.5)')`, chainID)
	require.NoError(t, err)

	// Simulate what chainWorker does before calling executeChain.
	require.True(t, pge.InsertChainRunStatus(ctx, chainID, 16), "chain run status should be inserted")

	// Cancel the context while the task is sleeping, mimicking notify_chain_stop().
	execCtx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	sch.executeChain(execCtx, Chain{ChainID: chainID, MaxInstances: 16})

	// The stale active_chain row must be gone: cleanup must succeed despite the
	// cancelled context.
	var count int
	err = pge.ConfigDb.QueryRow(ctx,
		"SELECT count(*) FROM timetable.active_chain WHERE chain_id = $1", chainID).Scan(&count)
	require.NoError(t, err)
	assert.Zero(t, count, "active_chain entry must be removed even when the execution context was cancelled")
}

func TestRun(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	pge := container.Engine
	require.NotNil(t, pge.ConfigDb, "ConfigDB should be initialized")

	err := pge.ExecuteCustomScripts(context.Background(), "../../samples/Exclusive.sql")
	assert.NoError(t, err, "Creating interval tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/Autonomous.sql")
	assert.NoError(t, err, "Creating autonomous tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/RemoteDB.sql")
	assert.NoError(t, err, "Creating remote tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/Basic.sql")
	assert.NoError(t, err, "Creating sql tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/NoOp.sql")
	assert.NoError(t, err, "Creating built-in tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/Shell.sql")
	assert.NoError(t, err, "Creating program tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/ManyTasks.sql")
	assert.NoError(t, err, "Creating many tasks failed")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())
	assert.NoError(t, sch.StartChain(context.Background(), 1))
	assert.ErrorContains(t, sch.StopChain(context.Background(), -1), "No running chain found")
	go func() {
		time.Sleep(10 * time.Second)
		sch.Shutdown()
	}()
	assert.Equal(t, 1, max(0, 1))
	assert.Equal(t, 1, max(1, 0))
	assert.True(t, sch.IsReady())
	assert.Equal(t, ShutdownStatus, sch.Run(context.Background()))
}
