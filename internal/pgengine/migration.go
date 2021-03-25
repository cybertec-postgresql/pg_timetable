package pgengine

import (
	"context"
	"embed"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/migrator"
	pgx "github.com/jackc/pgx/v4"
)

//go:embed sql/migrations/*.sql
var migrations embed.FS

var m *migrator.Migrator

// MigrateDb upgrades database with all migrations
func (pge *PgEngine) MigrateDb(ctx context.Context) bool {
	pge.l.Info("Upgrading database...")
	conn, err := pge.ConfigDb.Acquire(ctx)
	defer conn.Release()
	if err != nil {
		pge.l.WithError(err).Error("Cannot acquire database")
		return false
	}
	if err := m.Migrate(ctx, conn.Conn()); err != nil {
		pge.l.WithError(err).Error()
		return false
	}
	return true
}

// CheckNeedMigrateDb checks need of upgrading database and throws error if that's true
func (pge *PgEngine) CheckNeedMigrateDb(ctx context.Context) (bool, error) {
	if err := pge.initMigrator(); err != nil {
		return false, err
	}
	pge.l.Debug("Check need of upgrading database...")
	ctx = log.WithLogger(ctx, pge.l)
	conn, err := pge.ConfigDb.Acquire(ctx)
	defer conn.Release()
	if err != nil {
		return false, err
	}
	upgrade, err := m.NeedUpgrade(ctx, conn.Conn())
	if upgrade {
		pge.l.Error("You need to upgrade your database before proceeding, use --upgrade option")
	}
	if err != nil {
		pge.l.WithError(err).Error("Migration check failed")
	}
	return upgrade, err
}

func executeMigrationScript(ctx context.Context, tx pgx.Tx, fname string) error {
	sql, err := migrations.ReadFile(fname)
	if err != nil {
		_, err = tx.Exec(ctx, string(sql))
	}
	return err
}

func (pge *PgEngine) initMigrator() error {
	if m != nil {
		return nil
	}
	var err error
	m, err = migrator.New(
		migrator.TableName("timetable.migrations"),
		migrator.SetNotice(func(s string) {
			pge.l.Info(s)
		}),
		migrator.Migrations(
			&migrator.Migration{
				Name: "0051 Implement upgrade machinery",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					// "migrations" table will be created automatically
					return nil
				},
			},
			&migrator.Migration{
				Name: "0070 Interval scheduling and cron only syntax",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					return executeMigrationScript(ctx, tx, "00070.sql")
				},
			},
			&migrator.Migration{
				Name: "0086 Add task output to execution_log",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					_, err := tx.Exec(ctx, "ALTER TABLE timetable.execution_log "+
						"ADD COLUMN output TEXT")
					return err
				},
			},
			&migrator.Migration{
				Name: "0108 Add client_name column to timetable.run_status",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					return executeMigrationScript(ctx, tx, "00108.sql")
				},
			},
			&migrator.Migration{
				Name: "0122 Add autonomous tasks",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					_, err := tx.Exec(ctx, "ALTER TABLE timetable.task_chain "+
						"ADD COLUMN autonomous BOOLEAN NOT NULL DEFAULT false")
					return err
				},
			},
			&migrator.Migration{
				Name: "0105 Add next_run function",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					return executeMigrationScript(ctx, tx, "00105.sql")
				},
			},
			&migrator.Migration{
				Name: "0149 Reimplement session locking",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					return executeMigrationScript(ctx, tx, "00149.sql")
				},
			},
			&migrator.Migration{
				Name: "0155 Rename SHELL task kind to PROGRAM",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					_, err := tx.Exec(ctx, "ALTER TYPE timetable.task_kind RENAME VALUE 'SHELL' TO 'PROGRAM'")
					return err
				},
			},
			&migrator.Migration{
				Name: "0178 Disable tasks on a REPLICA node",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					return executeMigrationScript(ctx, tx, "00178.sql")
				},
			},
			&migrator.Migration{
				Name: "0195 Add notify_chain_start() and notify_chain_stop() functions",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					return executeMigrationScript(ctx, tx, "00195.sql")
				},
			},
			// adding new migration here, update "timetable"."migrations" in "sql_ddl.go"
		),
	)
	if err != nil {
		pge.l.WithError(err).Error("Cannot initialize migration")
	}
	return err
}
