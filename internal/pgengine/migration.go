package pgengine

import (
	"database/sql"
	"os"

	"github.com/cybertec-postgresql/pg_timetable/internal/migrator"
)

var m *migrator.Migrator

func migrateDb(db *sql.DB) {
	LogToDB("DEBUG", "Upgrading database...")
	if err := m.Migrate(ConfigDb.DB); err != nil {
		LogToDB("PANIC", err)
		os.Exit(3)
	}
}

func checkNeedMigrateDb(db *sql.DB) {
	LogToDB("DEBUG", "Check need of upgrading database...")
	upgrade, err := m.NeedUpgrade(ConfigDb.DB)
	if err != nil {
		LogToDB("PANIC", err)
		os.Exit(3)
	}
	if upgrade {
		LogToDB("ERROR", "You need to upgrade your database before proceeding, use --upgrade option")
		os.Exit(3)
	}
}

func init() {
	var err error
	m, err = migrator.New(
		migrator.TableName("timetable.migrations"),
		migrator.SetNotice(func(s string) {
			LogToDB("LOG", s)
		}),
		migrator.Migrations(
			&migrator.Migration{
				Name: "0051 Implement upgrade machinery",
				Func: func(tx *sql.Tx) error {
					// "migrations" table will be created automatically
					return nil
				},
			},
		),
	)
	if err != nil {
		LogToDB("ERROR", err)
	}
}
