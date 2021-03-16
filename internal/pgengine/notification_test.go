package pgengine_test

import (
	"context"

	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/stretchr/testify/assert"
)

// notify sends NOTIFY each second until context is available
func notify(ctx context.Context, t *testing.T, channel string, msg string) {
	conn, err := pge.ConfigDb.Acquire(ctx)
	if ctx.Err() == nil {
		assert.NoError(t, err)
	}
	defer conn.Release()
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.Tick(time.Second):
			_, err = conn.Exec(ctx, "SELECT pg_notify($1, $2)", channel, msg)
			if ctx.Err() == nil {
				assert.NoError(t, err)
			}
		}
	}
}

func TestNotifications(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go notify(ctx, t, "pgengine_unit_test", `{"ConfigID": 1234, "Command": "START", "Ts": 123456}`)
	assert.Equal(t, pgengine.ChainSignal{1234, "START", 123456}, pge.WaitForChainSignal(ctx), "Should return proper notify payload")
	assert.Equal(t, pgengine.ChainSignal{0, "", 0}, pge.WaitForChainSignal(ctx), "Should return 0 due to context deadline")
}

func TestHandleNotifications(t *testing.T) {
	teardownTestCase := SetupTestCaseEx(t, func(c *cmdparser.CmdOptions) {
		c.Debug = true
	})
	defer teardownTestCase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go pge.HandleNotifications(ctx)
	go notify(ctx, t, "pgengine_unit_test", `{"ConfigID": 4321, "Command": "STOP", "Ts": 654321}`)
	assert.Equal(t, pgengine.ChainSignal{4321, "STOP", 654321}, pge.WaitForChainSignal(ctx), "Should return proper notify payload")
	assert.Equal(t, pgengine.ChainSignal{}, pge.WaitForChainSignal(ctx), "Should return 0 due to context deadline")
}
