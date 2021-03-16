package main

import (
	"context"
	"os"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
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

func main() {
	ctx := context.Background()
	cmdOpts, err := cmdparser.Parse()
	if err != nil {
		pgengine.Log("PANIC", "Error parsing command line arguments: ", err)
		os.Exit(2)
	}
	connctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if pge, err = pgengine.New(connctx, *cmdOpts); err != nil {
		os.Exit(2)
	}
	defer pge.Finalize()
	if cmdOpts.Upgrade {
		if !pge.MigrateDb(ctx) {
			os.Exit(3)
		}
	} else {
		if upgrade, err := pge.CheckNeedMigrateDb(ctx); upgrade || err != nil {
			os.Exit(3)
		}
	}
	if cmdOpts.Init {
		os.Exit(0)
	}
	pge.SetupCloseHandler()
	sch := scheduler.New(pge)
	for sch.Run(ctx, cmdOpts.Debug) == scheduler.ConnectionDroppped {
		pge.ReconnectAndFixLeftovers(ctx)
	}
}
