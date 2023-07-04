package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

var pge *pgengine.PgEngine

// SetupTestCase used to connect and to initialize test PostgreSQL database
func SetupTestCase(t *testing.T) func(t *testing.T) {
	cmdOpts := config.NewCmdOptions("-c", "pgengine_unit_test", "--password=somestrong")
	t.Log("Setup test case")
	timeout := time.After(6 * time.Second)
	done := make(chan bool)
	go func() {
		pge, _ = pgengine.New(context.Background(), *cmdOpts, log.Init(config.LoggingOpts{LogLevel: "error"}))
		done <- true
	}()
	select {
	case <-timeout:
		t.Fatal("Cannot connect and initialize test database in time")
	case <-done:
	}
	return func(t *testing.T) {
		_, _ = pge.ConfigDb.Exec(context.Background(), "DROP SCHEMA IF EXISTS timetable CASCADE")
		t.Log("Test schema dropped")
	}
}

func TestRun(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)

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
	assert.Equal(t, 1, Max(0, 1))
	assert.Equal(t, 1, Max(1, 0))
	assert.True(t, sch.IsReady())
	assert.Equal(t, ShutdownStatus, sch.Run(context.Background()))
}
