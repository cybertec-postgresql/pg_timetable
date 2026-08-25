//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/notify"
)

const (
	serviceStopTimeout     = 30 * time.Second
	serviceRestartDelay    = time.Minute
	serviceRecoveryReset   = 86400 // seconds
	servicePollingInterval = 300 * time.Millisecond
	serviceDescription     = "Advanced scheduler for PostgreSQL"
)

func init() {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return
	}
	// Windows services always start in the system32 directory, so try to
	// switch into the directory where the pg_timetable executable is.
	if execPath, err := os.Executable(); err == nil {
		_ = os.Chdir(filepath.Dir(execPath))
	}
	go func() {
		_ = svc.Run("", winServiceRunner{})
	}()
}

type winServiceRunner struct{}

// Execute implements the svc.Handler interface and serves control requests
// from the Service Control Manager until the service is stopped.
func (winServiceRunner) Execute(_ []string, request <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	notify.SetGlobalStatus(status)
	status <- svc.Status{State: svc.StartPending}
	for req := range request {
		switch req.Cmd {
		case svc.Interrogate:
			status <- req.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			cancelApplication(status)
			return false, 0
		}
	}
	return false, 0
}

// cancelApplication triggers a graceful shutdown of the running daemon. The
// Service Control Manager may request a stop while main is still starting up
// and has not assigned the cancellation function yet, so wait until it is
// ready (bounded by serviceStopTimeout), reporting StopPending heartbeats so
// the SCM does not consider the service hung.
func cancelApplication(status chan<- svc.Status) {
	deadline := time.After(serviceStopTimeout)
	ticker := time.NewTicker(servicePollingInterval)
	defer ticker.Stop()
	for {
		if cancel := getCancelFn(); cancel != nil {
			cancel()
			return
		}
		select {
		case <-cancelReady:
			if cancel := getCancelFn(); cancel != nil {
				cancel()
			}
			return
		case <-deadline:
			// Give up waiting; the process will be terminated by the SCM.
			return
		case <-ticker.C:
			status <- svc.Status{State: svc.StopPending}
		}
	}
}

// handleServiceCommand performs the requested --service action and returns
// the exit code for the application.
func handleServiceCommand(cmdOpts *config.CmdOptions) int {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Cannot determine executable path:", err)
		return ExitCodeFatalError
	}
	switch cmdOpts.Service {
	case "install":
		return serviceInstall(cmdOpts.ServiceName, cmdOpts, exePath)
	case "uninstall":
		return serviceUninstall(cmdOpts.ServiceName)
	default:
		return serviceControl(cmdOpts.ServiceName, cmdOpts.Service)
	}
}

// connectManager opens a connection to the Service Control Manager.
func connectManager() (*mgr.Mgr, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to the Service Control Manager (are you running as Administrator?): %w", err)
	}
	return m, nil
}

// serviceInstall registers the executable as a Windows service. The command
// line arguments are persisted so that the service runs the daemon with the
// same connection settings used during installation.
func serviceInstall(name string, cmdOpts *config.CmdOptions, exePath string) int {
	if err := validateServiceAccount(cmdOpts.ServiceUser, cmdOpts.ServicePassword); err != nil {
		fmt.Println(err)
		return ExitCodeFatalError
	}
	m, err := connectManager()
	if err != nil {
		fmt.Println(err)
		return ExitCodeFatalError
	}
	defer m.Disconnect()

	if s, err := m.OpenService(name); err == nil {
		s.Close()
		fmt.Printf("Service %q already exists\n", name)
		return ExitCodeFatalError
	}
	args := filterServiceArgs(os.Args[1:])
	for _, warning := range relativePathWarnings(args) {
		fmt.Println("Warning:", warning)
	}
	s, err := m.CreateService(name, exePath, newServiceConfig(name, cmdOpts), args...)
	if err != nil {
		fmt.Println("Failed to install service:", err)
		return ExitCodeFatalError
	}
	defer s.Close()
	actions := []mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: serviceRestartDelay}}
	if err := s.SetRecoveryActions(actions, serviceRecoveryReset); err != nil {
		fmt.Println("Service installed, but recovery actions were not configured:", err)
	}
	fmt.Printf("Service %q installed successfully\n", name)
	return ExitCodeOK
}

// newServiceConfig builds the service configuration for installation. An
// empty ServiceUser means the default LocalSystem account.
func newServiceConfig(name string, cmdOpts *config.CmdOptions) mgr.Config {
	return mgr.Config{
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
		DisplayName:      name,
		Description:      serviceDescription,
		ServiceStartName: cmdOpts.ServiceUser,
		Password:         cmdOpts.ServicePassword,
	}
}

// validateServiceAccount checks the combination of the service account
// options before contacting the Service Control Manager.
func validateServiceAccount(user, password string) error {
	if user == "" && password > "" {
		return errors.New("--service-password requires --service-user")
	}
	return nil
}

// serviceUninstall stops (if needed) and removes the Windows service.
func serviceUninstall(name string) int {
	m, err := connectManager()
	if err != nil {
		fmt.Println(err)
		return ExitCodeFatalError
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		fmt.Printf("Service %q is not installed\n", name)
		return ExitCodeFatalError
	}
	defer s.Close()
	if err := stopService(s); err != nil {
		fmt.Printf("Failed to stop service %q: %v\n", name, err)
		return ExitCodeFatalError
	}
	if err := s.Delete(); err != nil {
		fmt.Println("Failed to uninstall service:", err)
		return ExitCodeFatalError
	}
	fmt.Printf("Service %q uninstalled\n", name)
	return ExitCodeOK
}

// serviceControl handles the start, stop, restart, and status actions.
func serviceControl(name, action string) int {
	m, err := connectManager()
	if err != nil {
		fmt.Println(err)
		return ExitCodeFatalError
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		fmt.Printf("Service %q is not installed\n", name)
		return ExitCodeFatalError
	}
	defer s.Close()

	switch action {
	case "start":
		err = s.Start()
		if err == nil {
			fmt.Printf("Service %q started\n", name)
		}
	case "stop":
		err = stopService(s)
		if err == nil {
			fmt.Printf("Service %q stopped\n", name)
		}
	case "restart":
		if err = stopService(s); err == nil {
			err = s.Start()
		}
		if err == nil {
			fmt.Printf("Service %q restarted\n", name)
		}
	case "status":
		var status svc.Status
		if status, err = s.Query(); err == nil {
			fmt.Printf("Service %q: %s\n", name, serviceStateString(status.State))
		}
	}
	if err != nil {
		fmt.Printf("Failed to %s service: %v\n", action, err)
		return ExitCodeFatalError
	}
	return ExitCodeOK
}

// stopService requests the service to stop and waits until it reports the
// stopped state or the timeout expires.
func stopService(s *mgr.Service) error {
	status, err := s.Control(svc.Stop)
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	for deadline := time.Now().Add(serviceStopTimeout); time.Now().Before(deadline); <-time.After(servicePollingInterval) {
		if status, err = s.Query(); err != nil {
			return err
		}
		if status.State == svc.Stopped {
			return nil
		}
	}
	return fmt.Errorf("timed out waiting for service to stop")
}

// serviceStateString returns a human-readable name for the service state.
func serviceStateString(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start pending"
	case svc.StopPending:
		return "stop pending"
	case svc.Running:
		return "running"
	case svc.ContinuePending:
		return "continue pending"
	case svc.PausePending:
		return "pause pending"
	case svc.Paused:
		return "paused"
	default:
		return "unknown"
	}
}

// filterServiceArgs removes the service management flags from the argument
// list so they are not persisted into the installed service command line.
// This also keeps the service account credentials out of the registry.
func filterServiceArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := strings.TrimLeft(arg, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		}
		switch name {
		case "service", "service-name", "service-user", "service-password":
			if !strings.Contains(arg, "=") {
				i++ // skip the option value too
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// filePathFlags maps flags whose values denote file paths.
var filePathFlags = map[string]bool{
	"f":        true,
	"file":     true,
	"config":   true,
	"log-file": true,
}

// relativePathWarnings returns warnings for path arguments that would be
// resolved against the executable directory when running as a service.
func relativePathWarnings(args []string) []string {
	var warnings []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value := strings.TrimLeft(arg, "-"), ""
		hasValue := false
		if before, after, ok := strings.Cut(arg, "="); ok {
			name, value, hasValue = strings.TrimLeft(before, "-"), after, true
		} else if filePathFlags[name] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			value, hasValue = args[i+1], true
			i++
		}
		if hasValue && filePathFlags[name] && !filepath.IsAbs(value) {
			warnings = append(warnings, fmt.Sprintf(
				"path %q given for --%s is relative and will be resolved against the executable directory when running as a service",
				value, name))
		}
	}
	return warnings
}
