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

// below this line should appear migration funtions only

func migration70(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE DOMAIN timetable.cron AS TEXT CHECK(
	substr(VALUE, 1, 6) IN ('@every', '@after') AND (substr(VALUE, 7) :: INTERVAL) IS NOT NULL	
	OR VALUE IN ('@annually', '@yearly', '@monthly', '@weekly', '@daily', '@hourly', '@reboot')
	OR VALUE ~ '^(((\d+,)+\d+|(\d+(\/|-)\d+)|(\*(\/|-)\d+)|\d+|\*) +){4}(((\d+,)+\d+|(\d+(\/|-)\d+)|(\*(\/|-)\d+)|\d+|\*) ?)$'
);

ALTER TABLE timetable.chain_execution_config
	ADD COLUMN run_at timetable.cron;

UPDATE timetable.chain_execution_config 
	SET run_at = 
		COALESCE(by_minute, '*') ||
		COALESCE(by_hour, '*') ||
		COALESCE(by_day, '*') ||
		COALESCE(by_month, '*') ||
		COALESCE(by_day_of_week, '*');

ALTER TABLE timetable.chain_execution_config
	DROP COLUMN by_minute,
	DROP COLUMN by_hour,
	DROP COLUMN by_day,
	DROP COLUMN by_month,
	DROP COLUMN by_day_of_week;
	`); err != nil {
		return err
	}
	return nil
}
