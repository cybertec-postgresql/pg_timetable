package client_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTailLogs_ReceivesNewEntries verifies that TailLogs picks up rows inserted
// after it starts (REQ-013 / P5-1).
func TestTailLogs_ReceivesNewEntries(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	c := newConnectedClient(t, tc)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	var mu sync.Mutex
	var received []client.LogEntry

	// Start the tail in a goroutine; it returns when ctx is cancelled.
	tailDone := make(chan error, 1)
	go func() {
		tailDone <- c.TailLogs(ctx, client.LogFilter{ClientName: "tail-test"}, func(e client.LogEntry) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		})
	}()

	// Give the first tick a moment to fire, then insert rows.
	time.Sleep(1200 * time.Millisecond)
	for i := range 3 {
		_, err := tc.Engine.ConfigDb.Exec(context.Background(),
			`INSERT INTO timetable.log (pid, log_level, client_name, message)
			 VALUES ($1, 'INFO', 'tail-test', $2)`, i+1, "tail-msg")
		require.NoError(t, err)
	}

	// Wait up to 4 seconds for the 3 rows to be picked up (≤ 4 poll ticks).
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	cancel() // trigger graceful shutdown

	err := <-tailDone
	assert.NoError(t, err, "TailLogs must return nil on context cancellation (P5-2)")

	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, len(received), 3, "all inserted log entries should be received")
	for _, e := range received {
		assert.Equal(t, "tail-test", e.ClientName)
		assert.Equal(t, "tail-msg", e.Message)
	}
}

// TestTailLogs_FilterByClient verifies the client_name filter works in tail mode.
func TestTailLogs_FilterByClient(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	c := newConnectedClient(t, tc)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// Insert one row for target client, one for another.
	_, err := tc.Engine.ConfigDb.Exec(context.Background(),
		`INSERT INTO timetable.log (pid, log_level, client_name, message)
		 VALUES (1, 'INFO', 'wanted', 'yes'), (2, 'INFO', 'other', 'no')`)
	require.NoError(t, err)

	var mu sync.Mutex
	var received []client.LogEntry
	tailDone := make(chan error, 1)
	go func() {
		tailDone <- c.TailLogs(ctx, client.LogFilter{ClientName: "wanted"}, func(e client.LogEntry) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		})
	}()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	cancel()
	<-tailDone

	mu.Lock()
	defer mu.Unlock()
	for _, e := range received {
		assert.Equal(t, "wanted", e.ClientName, "should only receive rows for 'wanted'")
	}
}

// TestTailLogs_GracefulCancel verifies that cancelling the context causes
// TailLogs to return nil promptly (P5-2).
func TestTailLogs_GracefulCancel(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	c := newConnectedClient(t, tc)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.TailLogs(ctx, client.LogFilter{}, func(client.LogEntry) {})
	}()

	// Cancel immediately after one tick.
	time.Sleep(1200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("TailLogs did not return after context cancellation")
	}
}
