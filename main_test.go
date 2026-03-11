package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// newTestLogger returns a silent logger suitable for use in tests.
func newTestLogger() log.LoggerHookerIface {
	return log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"})
}

// setupTestContainer starts a bare PostgreSQL container and returns the
// connection string along with a cleanup function. Unlike the shared
// testutils helper, it does NOT initialise the pg_timetable schema so that
// run() can perform that step itself.
func setupTestContainer(t *testing.T) (connStr string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	c, err := postgres.Run(
		ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("timetable"),
		postgres.WithUsername("scheduler"),
		postgres.WithPassword("somestrong"),
		testcontainers.WithWaitStrategyAndDeadline(
			60*time.Second,
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	require.NoError(t, err, "Failed to start PostgreSQL container")
	cs, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = c.Terminate(ctx)
		t.Fatalf("Failed to get connection string: %v", err)
	}
	return cs, func() { _ = c.Terminate(ctx) }
}

// TestPrintVersion verifies that printVersion writes the expected fields to
// stdout.
func TestPrintVersion(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	oldStdout := os.Stdout
	os.Stdout = w

	printVersion()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	assert.Contains(t, out, "pg_timetable:")
	assert.Contains(t, out, "Version:")
	assert.Contains(t, out, "DB Schema:")
	assert.Contains(t, out, "Git Commit:")
	assert.Contains(t, out, "Built:")
}

// TestSetupCloseHandler verifies that sending SIGTERM causes the provided
// cancel function to be called. Skipped on Windows where signal delivery to
// the current process works differently.
func TestSetupCloseHandler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM delivery to self is not supported on Windows")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	SetupCloseHandler(func() {
		cancel()
		close(done)
	})

	p, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, p.Signal(syscall.SIGTERM))

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel was not called within 3 s of receiving SIGTERM")
	}
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

// TestRunDBConnectionFailure verifies that run returns ExitCodeDBEngineError
// when the database is unreachable.
func TestRunDBConnectionFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	cmdOpts := config.NewCmdOptions(
		"--clientname=test_conn_fail",
		// port 1 is almost universally refused immediately
		"--connstr=postgres://invalid:invalid@localhost:1/invalid?sslmode=disable",
	)
	code := run(ctx, cmdOpts, newTestLogger())
	assert.Equal(t, ExitCodeDBEngineError, code)
}

// TestRunInitOnly verifies that run initialises the database schema and exits
// cleanly when the --init flag is supplied.
func TestRunInitOnly(t *testing.T) {
	connStr, cleanup := setupTestContainer(t)
	defer cleanup()

	cmdOpts := config.NewCmdOptions(
		"--clientname=test_main_init",
		"--connstr="+connStr,
		"--init",
	)
	code := run(context.Background(), cmdOpts, newTestLogger())
	assert.Equal(t, ExitCodeOK, code)
}

// TestRunUpgrade verifies that run performs a schema upgrade and exits cleanly
// when the --upgrade flag is combined with --init.
func TestRunUpgrade(t *testing.T) {
	connStr, cleanup := setupTestContainer(t)
	defer cleanup()

	cmdOpts := config.NewCmdOptions(
		"--clientname=test_main_upgrade",
		"--connstr="+connStr,
		"--upgrade",
		"--init",
	)
	code := run(context.Background(), cmdOpts, newTestLogger())
	assert.Equal(t, ExitCodeOK, code)
}

// TestRunContextCancellation verifies that run returns ExitCodeOK (not
// ExitCodeShutdownCommand) when the context is cancelled while the scheduler
// is running.
func TestRunContextCancellation(t *testing.T) {
	connStr, cleanup := setupTestContainer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmdOpts := config.NewCmdOptions(
		"--clientname=test_main_cancel",
		"--connstr="+connStr,
	)
	code := run(ctx, cmdOpts, newTestLogger())
	assert.Equal(t, ExitCodeOK, code)
}
