package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cybertec-postgresql/pg_timetable/internal/api"
	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/scheduler"
)

/**
 * pg_timetable is the daemon application responsible to execute scheduled SQL tasks that cannot be triggered by the
 * PostgreSQL server (PostgreSQL does not support time triggers).
 *
 * This application may run in the same machine as PostgreSQL server and must grant full access permission to the
 * timetable tables.
 */
var pge *pgengine.PgEngine

// SetupCloseHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS. We then handle this by calling
// our clean up procedure and exiting the program.
func SetupCloseHandler(cancel context.CancelFunc) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()
	exitCode = ExitCodeUserCancel
}

const (
	ExitCodeOK int = iota
	ExitCodeConfigError
	ExitCodeDBEngineError
	ExitCodeUpgradeError
	ExitCodeUserCancel
	ExitCodeShutdownCommand
)

var exitCode = ExitCodeOK

// version output variables
var (
	commit  string = "000000"
	version string = "master"
	date    string = "unknown"
	dbapi   string = "00436"
)

func printVersion() {
	fmt.Printf(`pg_timetable:
  Version:      %s
  DB Schema:    %s
  Git Commit:   %s
  Built:        %s
`, version, dbapi, commit, date)
}

func main() {
	defer func() { os.Exit(exitCode) }()

	ctx, cancel := context.WithCancel(context.Background())
	SetupCloseHandler(cancel)
	defer cancel()

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

	logger := log.Init(cmdOpts.Logging)
	apiserver := api.Init(cmdOpts.RestApi, logger)

	if pge, err = pgengine.New(ctx, *cmdOpts, logger); err != nil {
		logger.WithError(err).Error("Connection failed")
		exitCode = ExitCodeDBEngineError
		return
	}
	defer pge.Finalize()

	if cmdOpts.Start.Upgrade {
		if err := pge.MigrateDb(ctx); err != nil {
			logger.WithError(err).Error("Upgrade failed")
			exitCode = ExitCodeUpgradeError
			return
		}
	} else {
		if upgrade, err := pge.CheckNeedMigrateDb(ctx); upgrade || err != nil {
			if upgrade {
				logger.Error("You need to upgrade your database before proceeding, use --upgrade option")
			}
			if err != nil {
				logger.WithError(err).Error("Migration check failed")
			}
			exitCode = ExitCodeUpgradeError
			return
		}
	}
	if cmdOpts.Start.Init {
		return
	}
	sch := scheduler.New(pge, logger)
	apiserver.ApiHandler = sch

	if sch.Run(ctx) == scheduler.ShutdownStatus {
		exitCode = ExitCodeShutdownCommand
	}
}
