package pgengine

import (
	"context"
	"embed"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/migrator"
	pgx "github.com/jackc/pgx/v5"
)

//go:embed sql/migrations/*.sql
var migrationsFiles embed.FS

// MigrateDb upgrades database with all migrations
func (pge *PgEngine) MigrateDb(ctx context.Context) error {
	m, err := pge.initMigrator()
	if err != nil {
		return err
	}
	pge.l.Info("Upgrading database...")
	conn, err := pge.ConfigDb.Acquire(ctx)
	defer conn.Release()
	if err != nil {
		return err
	}
	if err := m.Migrate(ctx, conn.Conn()); err != nil {
		return err
	}
	return nil
}

// CheckNeedMigrateDb checks need of upgrading database and throws error if that's true
func (pge *PgEngine) CheckNeedMigrateDb(ctx context.Context) (bool, error) {
	m, err := pge.initMigrator()
	if err != nil {
		return false, err
	}
	pge.l.Debug("Check need of upgrading database...")
	ctx = log.WithLogger(ctx, pge.l)
	conn, err := pge.ConfigDb.Acquire(ctx)
	defer conn.Release()
	if err != nil {
		return false, err
	}
	return m.NeedUpgrade(ctx, conn.Conn())
}

// ExecuteMigrationScript executes the migration script specified by fname within transaction tx
func ExecuteMigrationScript(ctx context.Context, tx pgx.Tx, fname string) error {
	sql, err := migrationsFiles.ReadFile("sql/migrations/" + fname)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, string(sql))
	return err
}

// Migrations holds function returning all updgrade migrations needed
var Migrations func() migrator.Option = func() migrator.Option {
	return migrator.Migrations(
		&migrator.Migration{
			Name: "00259 Restart migrations for v4",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				// "migrations" table will be created automatically
				return nil
			},
		},
		&migrator.Migration{
			Name: "00305 Fix timetable.is_cron_in_time",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				return ExecuteMigrationScript(ctx, tx, "00305.sql")
			},
		},
		&migrator.Migration{
			Name: "00323 Append timetable.delete_job function",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				return ExecuteMigrationScript(ctx, tx, "00323.sql")
			},
		},
		&migrator.Migration{
			Name: "00329 Migration required for some new added functions",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				return ExecuteMigrationScript(ctx, tx, "00329.sql")
			},
		},
		&migrator.Migration{
			Name: "00334 Refactor timetable.task as plain schema without tree-like dependencies",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				return ExecuteMigrationScript(ctx, tx, "00334.sql")
			},
		},
		&migrator.Migration{
			Name: "00381 Rewrite active chain handling",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				return ExecuteMigrationScript(ctx, tx, "00381.sql")
			},
		},
		&migrator.Migration{
			Name: "00394 Add started_at column to active_session and active_chain tables",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				return ExecuteMigrationScript(ctx, tx, "00394.sql")
			},
		},
		&migrator.Migration{
			Name: "00417 Rename LOG database log level to INFO",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				return ExecuteMigrationScript(ctx, tx, "00417.sql")
			},
		},
		&migrator.Migration{
			Name: "00436 Add txid column to timetable.execution_log",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				return ExecuteMigrationScript(ctx, tx, "00436.sql")
			},
		},
		// adding new migration here, update "timetable"."migration" in "sql/ddl.sql"
		// and "dbapi" variable in main.go!

		// &migrator.Migration{
		// 	Name: "000XX Short description of a migration",
		// 	Func: func(ctx context.Context, tx pgx.Tx) error {
		// 		return executeMigrationScript(ctx, tx, "000XX.sql")
		// 	},
		// },
	)
}

func (pge *PgEngine) initMigrator() (*migrator.Migrator, error) {
	m, err := migrator.New(
		migrator.TableName("timetable.migration"),
		migrator.SetNotice(func(s string) {
			pge.l.Info(s)
		}),
		Migrations(),
	)
	if err != nil {
		pge.l.WithError(err).Error("Cannot initialize migration")
	}
	return m, err
}
