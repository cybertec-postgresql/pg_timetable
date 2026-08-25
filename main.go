package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/cybertec-postgresql/pg_timetable/internal/api"
	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/notify"
	"github.com/cybertec-postgresql/pg_timetable/internal/otel"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/scheduler"
)

/**
 * pg_timetable is the daemon application responsible to execute scheduled SQL tasks that cannot be triggered by the
 * PostgreSQL server (PostgreSQL does not support time triggers).
 *
 * This application may run on the same machine as PostgreSQL server and must grant full access permission to the
 * timetable tables.
 */
var pge *pgengine.PgEngine

// cancelFn stores the cancellation function for the application context.
// It is set by SetupCloseHandler and invoked by the Windows service handler
// when the Service Control Manager requests the service to stop.
//
// The Windows service handler runs on a separate goroutine (started from
// init) that may read cancelFn before or concurrently with main assigning
// it, so cancelFn is an atomic pointer. cancelReady is closed exactly once
// (guarded by cancelOnce) when the cancel function first becomes available,
// letting the service handler wait for a graceful shutdown target instead of
// silently dropping an early stop request.
var (
	cancelFn    atomic.Pointer[context.CancelFunc]
	cancelReady = make(chan struct{})
	cancelOnce  = new(sync.Once)
)

// setCancelFn stores the application cancellation function and signals, once,
// that it is ready to be used by other goroutines.
func setCancelFn(cancel context.CancelFunc) {
	if cancel == nil {
		cancelFn.Store(nil)
		return
	}
	cancelFn.Store(&cancel)
	cancelOnce.Do(func() { close(cancelReady) })
}

// getCancelFn returns the current application cancellation function safely.
func getCancelFn() context.CancelFunc {
	if p := cancelFn.Load(); p != nil {
		return *p
	}
	return nil
}

// SetupCloseHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS. We then handle this by calling
// our clean up procedure and exiting the program.
func SetupCloseHandler(cancel context.CancelFunc) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	setCancelFn(cancel)
	go func() {
		<-c
		cancel()
		exitCode = ExitCodeUserCancel
	}()
}

const (
	ExitCodeOK int = iota
	ExitCodeConfigError
	ExitCodeDBEngineError
	ExitCodeUpgradeError
	ExitCodeUserCancel
	ExitCodeShutdownCommand
	ExitCodeFatalError
)

var exitCode = ExitCodeOK

// version output variables
var (
	commit  = "000000"
	version = "master"
	date    = "unknown"
	dbapi   = "00820"
)

func printVersion() {
	fmt.Printf(`pg_timetable:
  Version:      %s
  DB Schema:    %s
  Git Commit:   %s
  Built:        %s
`, version, dbapi, commit, date)
}

// run contains the core application logic and returns an exit code.
func run(ctx context.Context, cmdOpts *config.CmdOptions, logger log.LoggerHookerIface) int {
	apiserver := api.Init(cmdOpts.RESTApi, logger)

	var err error
	if pge, err = pgengine.New(ctx, *cmdOpts, logger); err != nil {
		logger.WithError(err).Error("Connection failed")
		return ExitCodeDBEngineError
	}
	defer pge.Finalize()

	if cmdOpts.Start.Upgrade {
		if err := pge.MigrateDb(ctx); err != nil {
			logger.WithError(err).Error("Upgrade failed")
			return ExitCodeUpgradeError
		}
	} else {
		if upgrade, err := pge.CheckNeedMigrateDb(ctx); upgrade || err != nil {
			if upgrade {
				logger.Error("You need to upgrade your database before proceeding, use --upgrade option")
			}
			if err != nil {
				logger.WithError(err).Error("Migration check failed")
			}
			return ExitCodeUpgradeError
		}
	}
	if cmdOpts.Start.Init {
		return ExitCodeOK
	}

	// Verify the secret-store configuration before any chain runs.
	// Failures of the check itself are logged, not fatal — see
	// CheckSecretConfig.
	if err := pge.CheckSecretConfig(ctx); err != nil {
		logger.WithError(err).Warn("Secret configuration check failed")
	}

	// Initialise OTel provider (noop when not configured)
	otelProvider, otelErr := otel.New(ctx, cmdOpts.OTel, cmdOpts.ClientName, version)
	if otelErr != nil {
		logger.WithError(otelErr).Warn("OTel provider init failed; continuing without telemetry")
		otelProvider = otel.NewNoop()
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(),
			otelProvider.ShutdownTimeout())
		defer shutdownCancel()
		if err := otelProvider.Shutdown(shutdownCtx); err != nil {
			logger.WithError(err).Warn("OTel provider shutdown failed")
		}
	}()

	sch := scheduler.New(pge, logger, otelProvider)
	apiserver.APIHandler = sch

	notify.Ready()
	if sch.Run(ctx) == scheduler.ShutdownStatus {
		return ExitCodeShutdownCommand
	}
	return ExitCodeOK
}

func main() {
	cmdOpts, err := config.NewConfig(os.Stdout)
	if err != nil {
		if cmdOpts != nil && cmdOpts.VersionOnly() {
			printVersion()
			return
		}
		fmt.Println("Configuration error: ", err)
		exitCode = ExitCodeConfigError
		return
	}
	if cmdOpts.Version {
		printVersion()
	}

	if cmdOpts.Service > "" {
		os.Exit(handleServiceCommand(cmdOpts))
	}

	logger := log.Init(cmdOpts.Logging)
	ctx, cancel := context.WithCancel(context.Background())
	SetupCloseHandler(cancel)
	defer func() {
		cancel()
		if err := recover(); err != nil {
			exitCode = ExitCodeFatalError
			logger.WithField("callstack", string(debug.Stack())).Error(err)
		}
		os.Exit(exitCode)
	}()

	exitCode = run(ctx, cmdOpts, logger)
}
