//go:build windows

// Package notify reports application lifecycle states to the Windows Service
// Control Manager. It is a no-op on other platforms and when the process is
// not started by the service manager.
package notify

import "golang.org/x/sys/windows/svc"

// globalStatus stores the channel through which status updates are sent to
// the SCM. It is assigned by the service handler upon startup and remains
// nil when the process runs interactively.
var globalStatus chan<- svc.Status

// SetGlobalStatus assigns the channel through which status updates are sent
// to the SCM. It is called by the service control handler.
func SetGlobalStatus(status chan<- svc.Status) {
	globalStatus = status
}

// Ready notifies the SCM that the application finished its startup and is
// running, accepting stop and shutdown requests.
func Ready() {
	if globalStatus == nil {
		return
	}
	globalStatus <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}
}

// Stopping notifies the SCM that the application is shutting down.
func Stopping() {
	if globalStatus == nil {
		return
	}
	globalStatus <- svc.Status{State: svc.StopPending}
}
