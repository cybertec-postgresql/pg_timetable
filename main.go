package main

import (
	"context"
	"os"

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
	if cmdparser.Parse() != nil {
		os.Exit(2)
	}
	if !pgengine.InitAndTestConfigDBConnection(context.Background()) {
		os.Exit(2)
	}
	if pgengine.Upgrade {
		if !pgengine.MigrateDb(context.Background()) {
			os.Exit(3)
		}
	} else {
		if upgrade, err := pgengine.CheckNeedMigrateDb(context.Background()); upgrade || err != nil {
			os.Exit(3)
		}
	}
	defer pgengine.FinalizeConfigDBConnection()
	pgengine.SetupCloseHandler()
	scheduler.Run()
}
