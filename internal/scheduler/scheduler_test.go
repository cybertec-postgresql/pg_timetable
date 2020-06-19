package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/scheduler"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
)

func TestMain(m *testing.M) {
	testutils.TestMain(m)
}

func TestRun(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NotNil(t, pgengine.ConfigDb, "ConfigDB should be initialized")

	ok := pgengine.ExecuteCustomScripts(context.Background(), "../../samples/Interval.sql")
	assert.True(t, ok, "Creating interval tasks failed")
	ok = pgengine.ExecuteCustomScripts(context.Background(), "../../samples/basic.sql")
	assert.True(t, ok, "Creating sql tasks failed")
	ok = pgengine.ExecuteCustomScripts(context.Background(), "../../samples/NoOp.sql")
	assert.True(t, ok, "Creating built-in tasks failed")
	ok = pgengine.ExecuteCustomScripts(context.Background(), "../../samples/Shell.sql")
	assert.True(t, ok, "Creating shell tasks failed")
	assert.Equal(t, scheduler.Run(ctx), scheduler.ContextCancelled)

}
