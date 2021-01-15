package pgengine_test

import (
	"context"

	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestNotifications(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		for range time.Tick(time.Second) {
			_, err := pgengine.ConfigDb.ExecContext(ctx, `NOTIFY pgengine_unit_test, '{"ConfigID": 1234, "Command": "START", "Ts": 123456}'`)
			if ctx.Err() == nil {
				assert.NoError(t, err)
			}
		}
	}()
	assert.Equal(t, pgengine.ChainSignal{1234, "START", 123456}, pgengine.WaitForChainSignal(ctx), "Should return proper notify payload")
	assert.Equal(t, pgengine.ChainSignal{0, "", 0}, pgengine.WaitForChainSignal(ctx), "Should return 0 due to context deadline")
}

func TestHandleNotifications(t *testing.T) {
	teardownTestCase := testutils.SetupTestCaseEx(t, func(c *cmdparser.CmdOptions) {
		c.Verbose = true
		c.Debug = true
	})
	defer teardownTestCase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		for range time.Tick(time.Second) {
			_, err := pgengine.ConfigDb.ExecContext(ctx, `NOTIFY pgengine_unit_test, '{"ConfigID": 1234, "Command": "START", "Ts": 123456}'`)
			if ctx.Err() == nil {
				assert.NoError(t, err)
			}
		}
	}()
	go pgengine.HandleNotifications(ctx)
	assert.Equal(t, pgengine.ChainSignal{1234, "START", 123456}, pgengine.WaitForChainSignal(ctx), "Should return proper notify payload")
	assert.Equal(t, pgengine.ChainSignal{0, "", 0}, pgengine.WaitForChainSignal(ctx), "Should return 0 due to context deadline")
}
