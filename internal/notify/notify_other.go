//go:build !windows

// Package notify reports application lifecycle states to the Windows Service
// Control Manager. It is a no-op on other platforms and when the process is
// not started by the service manager.
package notify

// SetGlobalStatus is a no-op on non-Windows platforms.
func SetGlobalStatus(any) {}

// Ready is a no-op on non-Windows platforms.
func Ready() {}

// Stopping is a no-op on non-Windows platforms.
func Stopping() {}
