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
			&migrator.Migration{
				Name: "0070 Interval scheduling and cron only syntax",
				Func: migration70,
			},
			&migrator.Migration{
				Name: "0086 Add task output to execution_log",
				Func: func(tx *sql.Tx) error {
					_, err := tx.Exec("ALTER TABLE timetable.execution_log " +
						"ADD COLUMN output TEXT")
					return err
				},
			},
			&migrator.Migration{
				Name: "0108 Add client_name column to timetable.run_status",
				Func: migration108,
			},
			// adding new migration here, update "timetable"."migrations" in "sql_ddl.go"
		),
	)
	if err != nil {
		LogToDB("ERROR", err)
	}
}

// below this line should appear migration funсtions only

func migration108(tx *sql.Tx) error {
	// first set <unknown> for existing rows, then drop default to force application to set it
	_, err := tx.Exec(`
ALTER TABLE timetable.execution_log
	ADD COLUMN client_name TEXT NOT NULL DEFAULT '<unknown>';
ALTER TABLE timetable.run_status
	ADD COLUMN client_name TEXT NOT NULL DEFAULT '<unknown>';
ALTER TABLE timetable.execution_log
	ALTER COLUMN client_name DROP DEFAULT;
ALTER TABLE timetable.run_status
	ALTER COLUMN client_name DROP DEFAULT;`)
	return err
}

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
	SET run_at = format('%s %s %s %s %s', 
		COALESCE(run_at_minute :: TEXT, '*'),
		COALESCE(run_at_hour :: TEXT, '*'),
		COALESCE(run_at_day :: TEXT, '*'),
		COALESCE(run_at_month :: TEXT, '*'),
		COALESCE(run_at_day_of_week :: TEXT, '*')
	);

ALTER TABLE timetable.chain_execution_config
	DROP COLUMN run_at_minute,
	DROP COLUMN run_at_hour,
	DROP COLUMN run_at_day,
	DROP COLUMN run_at_month,
	DROP COLUMN run_at_day_of_week;

CREATE OR REPLACE FUNCTION timetable.is_cron_in_time(run_at timetable.cron, ts timestamptz) RETURNS BOOLEAN AS
$$
DECLARE 
    a_by_minute integer[];
    a_by_hour integer[];
    a_by_day integer[];
    a_by_month integer[];
    a_by_day_of_week integer[]; 
BEGIN
    IF run_at IS NULL
    THEN
        RETURN TRUE;
    END IF;
    a_by_minute := timetable.cron_element_to_array(run_at, 'minute');
    a_by_hour := timetable.cron_element_to_array(run_at, 'hour');
    a_by_day := timetable.cron_element_to_array(run_at, 'day');
    a_by_month := timetable.cron_element_to_array(run_at, 'month');
    a_by_day_of_week := timetable.cron_element_to_array(run_at, 'day_of_week'); 
    RETURN  (a_by_month[1]       IS NULL OR date_part('month', ts) = ANY(a_by_month))
        AND (a_by_day_of_week[1] IS NULL OR date_part('dow', ts) = ANY(a_by_day_of_week))
        AND (a_by_day[1]         IS NULL OR date_part('day', ts) = ANY(a_by_day))
        AND (a_by_hour[1]        IS NULL OR date_part('hour', ts) = ANY(a_by_hour))
        AND (a_by_minute[1]      IS NULL OR date_part('minute', ts) = ANY(a_by_minute));    
END;
$$ LANGUAGE 'plpgsql';

DROP FUNCTION IF EXISTS timetable.job_add;

CREATE OR REPLACE FUNCTION timetable.job_add(
    task_name        TEXT,
    task_function    TEXT,
    client_name      TEXT,
    task_type        timetable.task_kind DEFAULT 'SQL'::timetable.task_kind,
    run_at           timetable.cron DEFAULT NULL,
    max_instances    INTEGER DEFAULT NULL,
    live             BOOLEAN DEFAULT false,
    self_destruct    BOOLEAN DEFAULT false
) RETURNS BIGINT AS
'WITH 
    cte_task(v_task_id) AS ( --Create task
        INSERT INTO timetable.base_task 
        VALUES (DEFAULT, task_name, task_type, task_function)
        RETURNING task_id
    ),
    cte_chain(v_chain_id) AS ( --Create chain
        INSERT INTO timetable.task_chain (task_id, ignore_error)
        SELECT v_task_id, TRUE FROM cte_task
        RETURNING chain_id
    )
INSERT INTO timetable.chain_execution_config (
    chain_id, 
    chain_name, 
    run_at, 
    max_instances, 
    live,
    self_destruct 
) SELECT 
    v_chain_id, 
    ''chain_'' || v_chain_id, 
    run_at,
    max_instances, 
    live, 
    self_destruct
FROM cte_chain
RETURNING chain_execution_config 
' LANGUAGE 'sql';`); err != nil {
		return err
	}
	return nil
}
