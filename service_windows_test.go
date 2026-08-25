//go:build windows

package main

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/notify"
)

// cancelStateSnapshot captures the package-level cancellation state so tests
// can mutate it and restore the original afterwards.
type cancelStateSnapshot struct {
	fn    *context.CancelFunc
	ready chan struct{}
	once  *sync.Once
}

// saveCancelState snapshots the current cancellation state.
func saveCancelState() cancelStateSnapshot {
	return cancelStateSnapshot{fn: cancelFn.Load(), ready: cancelReady, once: cancelOnce}
}

// restore puts the snapshotted cancellation state back.
func (s cancelStateSnapshot) restore() {
	cancelFn.Store(s.fn)
	cancelReady = s.ready
	cancelOnce = s.once
}

// resetCancelState clears the cancellation state to its pristine, unset form.
func resetCancelState() {
	cancelFn.Store(nil)
	cancelReady = make(chan struct{})
	cancelOnce = new(sync.Once)
}

func TestFilterServiceArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "equals form",
			args: []string{"--clientname=worker001", "--service=install", "postgresql://localhost/db"},
			want: []string{"--clientname=worker001", "postgresql://localhost/db"},
		},
		{
			name: "space form",
			args: []string{"--service", "install", "--clientname", "worker001"},
			want: []string{"--clientname", "worker001"},
		},
		{
			name: "single dash forms",
			args: []string{"-service", "uninstall"},
			want: []string{},
		},
		{
			name: "service name flag is dropped too",
			args: []string{"--service-name=my-svc", "--service", "status"},
			want: []string{},
		},
		{
			name: "account credentials are not persisted",
			args: []string{"--service-user", "DOMAIN\\svc_pgtt", "--service-password=secret", "-c", "worker001", "postgresql://localhost/tt"},
			want: []string{"-c", "worker001", "postgresql://localhost/tt"},
		},
		{
			name: "flags containing service substring are kept",
			args: []string{"--otel-service-name=traces", "-c", "worker001"},
			want: []string{"--otel-service-name=traces", "-c", "worker001"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, filterServiceArgs(tt.args))
		})
	}
}

func TestValidateServiceAccount(t *testing.T) {
	assert.NoError(t, validateServiceAccount("", ""))
	assert.NoError(t, validateServiceAccount("DOMAIN\\svc_pgtt", "secret"))
	assert.NoError(t, validateServiceAccount(".\\svc_pgtt", ""), "blank password is allowed for gMSA and built-in accounts")
	err := validateServiceAccount("", "secret")
	assert.ErrorContains(t, err, "--service-user")
}

func TestNewServiceConfig(t *testing.T) {
	cmdOpts := config.NewCmdOptions()
	cfg := newServiceConfig("pg_timetable", cmdOpts)
	assert.Empty(t, cfg.ServiceStartName, "no account given means LocalSystem")
	assert.Empty(t, cfg.Password)
	assert.True(t, cfg.DelayedAutoStart)
	assert.Equal(t, uint32(mgr.StartAutomatic), cfg.StartType, "service must start automatically")
	assert.Equal(t, "pg_timetable", cfg.DisplayName, "display name defaults to the service name")
	assert.Equal(t, serviceDescription, cfg.Description)

	cmdOpts = config.NewCmdOptions(
		"--service-name=my-svc",
		"--service-user=DOMAIN\\svc_pgtt",
		"--service-password=secret",
	)
	cfg = newServiceConfig("my-svc", cmdOpts)
	assert.Equal(t, "DOMAIN\\svc_pgtt", cfg.ServiceStartName)
	assert.Equal(t, "secret", cfg.Password)
	assert.Equal(t, "my-svc", cfg.DisplayName)
}

func TestRelativePathWarnings(t *testing.T) {
	assert.Empty(t, relativePathWarnings([]string{
		"--clientname=worker001",
		"--config=C:\\etc\\pg_timetable.yaml",
		"--log-file", "C:\\var\\log\\pg_timetable.log",
		"-f=C:\\sql\\init.sql",
	}))
	warnings := relativePathWarnings([]string{
		"--config=config.yaml",
		"--log-file=log.txt",
	})
	assert.Len(t, warnings, 2)
	assert.Contains(t, warnings[0], "config.yaml")
	assert.Contains(t, warnings[1], "log.txt")
}

func TestServiceStateString(t *testing.T) {
	assert.Equal(t, "running", serviceStateString(svc.Running))
	assert.Equal(t, "stopped", serviceStateString(svc.Stopped))
	assert.Equal(t, "start pending", serviceStateString(svc.StartPending))
	assert.Equal(t, "stop pending", serviceStateString(svc.StopPending))
	assert.Equal(t, "continue pending", serviceStateString(svc.ContinuePending))
	assert.Equal(t, "pause pending", serviceStateString(svc.PausePending))
	assert.Equal(t, "paused", serviceStateString(svc.Paused))
	assert.Equal(t, "unknown", serviceStateString(svc.State(99)))
}

// TestWinServiceRunnerExecute drives the service handler directly with fake
// SCM channels: it must report StartPending, answer interrogation, and on a
// stop request report StopPending and trigger the application cancellation.
func TestWinServiceRunnerExecute(t *testing.T) {
	requests := make(chan svc.ChangeRequest)
	statuses := make(chan svc.Status, 8)
	cancelled := make(chan struct{})

	prev := saveCancelState()
	setCancelFn(func() { close(cancelled) })
	defer func() {
		prev.restore()
		notify.SetGlobalStatus(nil)
	}()

	result := make(chan [2]uint32)
	go func() {
		shutdown, code := (winServiceRunner{}).Execute(nil, requests, statuses)
		result <- [2]uint32{boolToUint32(shutdown), code}
	}()

	select {
	case st := <-statuses:
		assert.Equal(t, svc.StartPending, st.State)
	case <-time.After(time.Second):
		t.Fatal("no initial status reported")
	}

	// Immediately after StartPending the handler must report Running and
	// advertise that it accepts Stop and Shutdown, otherwise the SCM keeps the
	// service in the Starting state and refuses control requests.
	select {
	case st := <-statuses:
		assert.Equal(t, svc.Running, st.State)
		assert.Equal(t, svc.AcceptStop|svc.AcceptShutdown, st.Accepts)
	case <-time.After(time.Second):
		t.Fatal("no running status reported")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Interrogate, CurrentStatus: svc.Status{State: svc.Running}}
	select {
	case st := <-statuses:
		assert.Equal(t, svc.Running, st.State)
	case <-time.After(time.Second):
		t.Fatal("interrogate request was not answered")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Stop}
	select {
	case st := <-statuses:
		assert.Equal(t, svc.StopPending, st.State)
	case <-time.After(time.Second):
		t.Fatal("no stop pending status reported")
	}

	res := <-result
	assert.Equal(t, uint32(0), res[0], "handler must not request process shutdown")
	assert.Equal(t, uint32(0), res[1], "exit code must be clean")
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancellation function was not called")
	}
}

// TestCancelApplicationWaitsForReadiness verifies that a stop request arriving
// before main has assigned the cancellation function does not get dropped:
// cancelApplication must wait until setCancelFn signals readiness and then run
// the cancellation, emitting StopPending heartbeats in the meantime.
func TestCancelApplicationWaitsForReadiness(t *testing.T) {
	prev := saveCancelState()
	resetCancelState()
	defer prev.restore()

	cancelled := make(chan struct{})
	statuses := make(chan svc.Status, 8)

	done := make(chan struct{})
	go func() {
		cancelApplication(statuses)
		close(done)
	}()

	// The handler must still be waiting because cancelFn is not ready yet.
	select {
	case <-done:
		t.Fatal("cancelApplication returned before the cancel function was ready")
	case <-time.After(50 * time.Millisecond):
	}

	setCancelFn(func() { close(cancelled) })

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancellation function was not called once ready")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelApplication did not return after cancelling")
	}
}

func boolToUint32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// requireSCM skips the test unless the process can reach the Service Control
// Manager, which normally means it is running with administrative rights.
// Unlike a hard failure, skipping keeps ordinary `go test` runs green on
// unprivileged machines.
func requireSCM(t *testing.T) {
	t.Helper()
	m, err := connectManager()
	if err != nil {
		t.Skipf("skipping test: %v", err)
	}
	_ = m.Disconnect()
}

// TestServiceControlMissingService checks that every control action, plus
// uninstall, reports ExitCodeFatalError rather than panicking when the target
// service is absent (or the SCM is unreachable). It needs no admin rights: it
// either exits early at connectManager or at OpenService, both of which return
// the fatal exit code, so it covers those error branches everywhere.
func TestServiceControlMissingService(t *testing.T) {
	const name = "pg_timetable_missing_svc_test"
	for _, action := range []string{"start", "stop", "restart", "status"} {
		t.Run(action, func(t *testing.T) {
			assert.Equal(t, ExitCodeFatalError, serviceControl(name, action))
		})
	}
	assert.Equal(t, ExitCodeFatalError, serviceUninstall(name),
		"uninstalling a missing service must fail")
}

// TestServiceInstallInvalidAccount covers the account-validation branch of
// serviceInstall, which rejects a password without a user before ever touching
// the SCM. It therefore runs regardless of privilege.
func TestServiceInstallInvalidAccount(t *testing.T) {
	cmdOpts := config.NewCmdOptions(
		"--service-password=secret",
		"postgresql://localhost/timetable",
	)
	assert.Equal(t, ExitCodeFatalError,
		serviceInstall("pg_timetable_invalid_acct_test", cmdOpts, "pg_timetable.exe"))
}

// TestHandleServiceCommandDispatch drives the public --service dispatcher for
// the non-mutating commands against a service that does not exist, covering the
// routing and executable-path lookup end to end. Without admin rights every
// branch exits fatally at connectManager; with rights they hit the "not
// installed" branch instead. Either way the exit code is fatal, and none of
// these actions create state. Install is covered separately because it mutates
// the system and must clean up after itself.
func TestHandleServiceCommandDispatch(t *testing.T) {
	for _, action := range []string{"uninstall", "status", "start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			cmdOpts := config.NewCmdOptions(
				"--service="+action,
				"--service-name=pg_timetable_missing_svc_test",
				"postgresql://localhost/timetable",
			)
			assert.Equal(t, ExitCodeFatalError, handleServiceCommand(cmdOpts))
		})
	}
}

// TestServiceLifecycle exercises pg_timetable's own install/query/uninstall
// path against a real service. Everything goes through the package's public
// surface (serviceInstall, serviceControl, serviceUninstall) so the test
// verifies pg_timetable behavior rather than the underlying svc/mgr library.
// Because it registers and deletes a real service, it is opt-in: set
// PGTT_TEST_SERVICES=1 and run with administrative rights to enable it.
func TestServiceLifecycle(t *testing.T) {
	if os.Getenv("PGTT_TEST_SERVICES") == "" {
		t.Skip("skipping test that modifies system services: set PGTT_TEST_SERVICES=1 to enable")
	}
	requireSCM(t)

	const name = "pg_timetable_lifecycle_test"
	exePath, err := os.Executable()
	require.NoError(t, err)

	cmdOpts := config.NewCmdOptions(
		"--service=install",
		"--service-name="+name,
		"postgresql://localhost/timetable",
	)

	// Clear any stale instance left by an interrupted previous run.
	_ = serviceUninstall(name)

	// Install through the real entry point and always clean up afterwards.
	require.Equal(t, ExitCodeOK, serviceInstall(name, cmdOpts, exePath), "install must succeed")
	t.Cleanup(func() { _ = serviceUninstall(name) })

	// A freshly installed service must answer status queries cleanly.
	assert.Equal(t, ExitCodeOK, serviceControl(name, "status"), "status of installed service")

	// Installing the same service twice must be rejected, not silently retried.
	assert.Equal(t, ExitCodeFatalError, serviceInstall(name, cmdOpts, exePath),
		"installing an existing service must fail")

	// Uninstall through the real entry point and confirm it is gone by asking
	// for its status again.
	require.Equal(t, ExitCodeOK, serviceUninstall(name), "uninstall must succeed")
	assert.Equal(t, ExitCodeFatalError, serviceControl(name, "status"),
		"service must be gone after uninstall")
}

// TestServiceRealStartStop answers "can we start a real application in tests?"
// with yes: it installs the test binary as a service and actually starts it.
// The test binary is svc.Run-aware because it shares this package's init(),
// which launches winServiceRunner when the SCM starts the process. TestMain
// (below) detects that service launch, installs a cancel function so Stop is
// instant, and blocks so the test suite itself does not run in the service
// process. The service therefore reaches Running and honours Start/Stop just
// like a production pg_timetable service.
//
// Opt-in and admin-only, like TestServiceLifecycle.
func TestServiceRealStartStop(t *testing.T) {
	if os.Getenv("PGTT_TEST_SERVICES") == "" {
		t.Skip("skipping test that modifies system services: set PGTT_TEST_SERVICES=1 to enable")
	}
	requireSCM(t)

	const name = "pg_timetable_realsvc_test"
	exePath, err := os.Executable()
	require.NoError(t, err)

	// Install a minimal service. The persisted arguments are irrelevant here
	// because TestMain short-circuits before the daemon ever connects to a DB.
	cmdOpts := config.NewCmdOptions("--service=install", "--service-name="+name)
	_ = serviceUninstall(name)
	require.Equal(t, ExitCodeOK, serviceInstall(name, cmdOpts, exePath), "install must succeed")
	t.Cleanup(func() { _ = serviceUninstall(name) })

	m, err := connectManager()
	require.NoError(t, err)
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(name)
	require.NoError(t, err)
	defer s.Close()

	// Start the real service and wait until the SCM reports it Running.
	require.Equal(t, ExitCodeOK, serviceControl(name, "start"), "start must succeed")
	require.Eventually(t, func() bool {
		st, qerr := s.Query()
		return qerr == nil && st.State == svc.Running
	}, 15*time.Second, servicePollingInterval, "service must reach Running")

	// Stop it through the real entry point; stopService waits for Stopped.
	require.Equal(t, ExitCodeOK, serviceControl(name, "stop"), "stop must succeed")
	st, err := s.Query()
	require.NoError(t, err)
	assert.Equal(t, svc.Stopped, st.State, "service must be stopped")

	require.Equal(t, ExitCodeOK, serviceUninstall(name), "uninstall must succeed")
}

// TestMain intercepts the case where the Service Control Manager launched this
// test binary as a Windows service (see TestServiceRealStartStop). In that
// process the package init() has already called svc.Run(winServiceRunner{}),
// which drives the service. We provide a no-op cancel function so a graceful
// Stop returns immediately, then wait for the service dispatcher to finish and
// exit the process. Exiting is essential: it releases the .test.exe file so
// the Go toolchain can delete it, and it prevents the normal test suite from
// running inside the service process. Any other invocation runs the tests as
// usual.
func TestMain(m *testing.M) {
	if isService, _ := svc.IsWindowsService(); isService {
		setCancelFn(func() {})
		<-serviceDone // wait until the SCM has stopped the service
		os.Exit(0)
	}
	os.Exit(m.Run())
}
