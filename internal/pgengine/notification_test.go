package pgengine_test

import (
	"context"

	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/stretchr/testify/assert"
)

// notify sends NOTIFY each second until context is available
func notifyAndCheck(ctx context.Context, conn pgengine.PgxIface, t *testing.T, channel string) {
	vals := []string{
		`{"ConfigID": 1, "Command": "STOP", "Ts": 1}`,              //{1, "STOP", 1, 0},
		`{"ConfigID": 2, "Command": "START", "Ts": 1}`,             //{2, "START", 1, 0},
		`{"ConfigID": 3, "Command": "START", "Ts": 2, "Delay": 1}`, //{3, "START", 2, 1},
		`{"ConfigID": 2, "Command": "START", "Ts": 1, "Delay": 0}`, //ignore duplicate message
		`{"ConfigID": 0, "Command": "START", "Ts": 3}`,             //ignore incorrect ConfigID
		`{"ConfigID": 4, "Command": "DRIVE", "Ts": 3}`,             //ignore incorrect Command
		`foo bazz`, //ignore corrupted json
	}
	for _, msg := range vals {
		_, err := conn.Exec(ctx, "SELECT pg_notify($1, $2)", channel, msg)
		assert.NoError(t, err)
	}

	var received int
	for {
		_ = pge.WaitForChainSignal(ctx)
		received++ //we will check only the number of received messages due to delay and race conditions
		select {
		case <-ctx.Done():
			assert.Equal(t, 4, received, "Should receive 3 proper messages + 1 final and ignore all other")
			return
		default:
			//continue
		}
	}
}

func TestHandleNotifications(t *testing.T) {
	teardownTestCase := SetupTestCaseEx(t, func(c *config.CmdOptions) {
		c.Start.Debug = true
	})
	defer teardownTestCase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go pge.HandleNotifications(ctx)
	time.Sleep(5 * time.Second)
	conn, err := pge.ConfigDb.Acquire(ctx) // HandleNotifications() uses blocking manner, so we want another connection
	assert.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(ctx, "UNLISTEN *") // do not interfere with the main handler
	assert.NoError(t, err)
	notifyAndCheck(ctx, pge.ConfigDb, t, "pgengine_unit_test")
}
