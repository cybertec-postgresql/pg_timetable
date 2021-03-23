package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

var pge *pgengine.PgEngine

//SetupTestCase used to connect and to initialize test PostgreSQL database
func SetupTestCase(t *testing.T) func(t *testing.T) {
	cmdOpts := cmdparser.NewCmdOptions("pgengine_unit_test")
	cmdOpts.Verbose = testing.Verbose()
	t.Log("Setup test case")
	timeout := time.After(6 * time.Second)
	done := make(chan bool)
	go func() {
		pge, _ = pgengine.New(context.Background(), *cmdOpts, log.Init("debug"))
		done <- true
	}()
	select {
	case <-timeout:
		t.Fatal("Cannot connect and initialize test database in time")
	case <-done:
	}
	return func(t *testing.T) {
		_, _ = pge.ConfigDb.Exec(context.Background(), "DROP SCHEMA IF EXISTS timetable CASCADE")
		pge.Finalize()
		t.Log("Test schema dropped")
	}
}

func TestRun(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)

	require.NotNil(t, pge.ConfigDb, "ConfigDB should be initialized")

	err := pge.ExecuteCustomScripts(context.Background(), "../../samples/Interval.sql")
	assert.NoError(t, err, "Creating interval tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/basic.sql")
	assert.NoError(t, err, "Creating sql tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/NoOp.sql")
	assert.NoError(t, err, "Creating built-in tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/Shell.sql")
	assert.NoError(t, err, "Creating program tasks failed")
	err = pge.ExecuteCustomScripts(context.Background(), "../../samples/SelfDestruct.sql")
	assert.NoError(t, err, "Creating program tasks failed")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	assert.Equal(t, New(pge, log.Init("debug")).Run(ctx, false), ContextCancelled)

}
