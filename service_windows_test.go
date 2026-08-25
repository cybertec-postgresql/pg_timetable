//go:build windows

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows/svc"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/notify"
)

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
