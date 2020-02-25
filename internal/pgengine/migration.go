package pgengine

import (
	"database/sql"
	"os"

	"github.com/cybertec-postgresql/pg_timetable/internal/migrator"
)

var m *migrator.Migrator

// MigrateDb upgrades database with all migrations
func MigrateDb() {
	LogToDB("LOG", "Upgrading database...")
	if err := m.Migrate(ConfigDb.DB); err != nil {
		LogToDB("PANIC", err)
		os.Exit(3)
	}
}

// CheckNeedMigrateDb checks need of upgrading database and throws error if that's true
func CheckNeedMigrateDb() {
	LogToDB("DEBUG", "Check need of upgrading database...")
	upgrade, err := m.NeedUpgrade(ConfigDb.DB)
	if upgrade {
		LogToDB("PANIC", "You need to upgrade your database before proceeding, use --upgrade option")
		defer os.Exit(3)
	}
	if err != nil {
		LogToDB("PANIC", err)
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
