package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

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
}

func main() {
	exitCode := 0
	defer func() { os.Exit(exitCode) }()

	ctx, cancel := context.WithCancel(context.Background())
	SetupCloseHandler(cancel)
	defer cancel()

	cmdOpts, err := config.NewConfig(os.Stdout)
	if err != nil {
		fmt.Println("Configuration error: ", err)
		exitCode = 1
		return
	}
	logger := log.Init(cmdOpts.Logging)

	connctx, conncancel := context.WithTimeout(ctx, 90*time.Second)
	defer conncancel()
	if pge, err = pgengine.New(connctx, *cmdOpts, logger); err != nil {
		exitCode = 2
		return
	}
	defer pge.Finalize()
	if cmdOpts.Start.Upgrade {
		if !pge.MigrateDb(ctx) {
			exitCode = 3
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
			exitCode = 3
			return
		}
	}
	if cmdOpts.Start.Init {
		return
	}
	sch := scheduler.New(pge, logger)
	for sch.Run(ctx) == scheduler.ConnectionDroppped {
		pge.ReconnectAndFixLeftovers(ctx)
	}
}
