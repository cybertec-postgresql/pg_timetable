package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
)

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
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "error"}))
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
