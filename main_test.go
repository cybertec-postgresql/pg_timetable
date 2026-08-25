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
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// requireDocker skips the current test unless a Docker daemon capable of
// running the Linux test containers is reachable through the same client
// testcontainers uses (honoring DOCKER_HOST, docker:// / tcp:// / npipe://
// endpoints and TESTCONTAINERS_* settings).
//
// The check is capability-based rather than OS-based: it queries the daemon's
// OSType and only skips when it is not "linux". A Windows developer running
// Docker Desktop with the WSL2/Linux backend therefore runs the full suite,
// while a Windows-container-only engine (as on the windows-latest CI runner)
// is skipped and covered by the Linux job instead.
func requireDocker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		t.Skipf("no Docker provider available; skipping testcontainer-based test: %v", err)
	}
	defer func() { _ = provider.Close() }()
	if err := provider.Health(ctx); err != nil {
		t.Skipf("Docker daemon not reachable; skipping testcontainer-based test: %v", err)
	}
	info, err := provider.Client().Info(ctx, client.InfoOptions{})
	if err != nil {
		t.Skipf("cannot query Docker daemon info; skipping testcontainer-based test: %v", err)
	}
	if info.Info.OSType != "linux" {
		t.Skipf("Docker daemon runs %q containers, not linux; skipping testcontainer-based test", info.Info.OSType)
	}
}

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
	requireDocker(t)
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
