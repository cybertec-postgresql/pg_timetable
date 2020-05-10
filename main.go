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

func main() {
	ctx := context.Background()
	if cmdparser.Parse() != nil {
		os.Exit(2)
	}
	connctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if !pgengine.InitAndTestConfigDBConnection(connctx) {
		os.Exit(2)
	}
	if pgengine.Upgrade {
		if !pgengine.MigrateDb(ctx) {
			os.Exit(3)
		}
	} else {
		if upgrade, err := pgengine.CheckNeedMigrateDb(ctx); upgrade || err != nil {
			os.Exit(3)
		}
	}
	defer pgengine.FinalizeConfigDBConnection()
	pgengine.SetupCloseHandler()
	for scheduler.Run(ctx) == scheduler.ConnectionDroppped {
		pgengine.ReconnectDbAndFixLeftovers(ctx)
	}
}
