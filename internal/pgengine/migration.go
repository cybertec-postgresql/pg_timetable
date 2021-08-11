package pgengine

import (
	"context"
	"embed"
	"errors"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/migrator"
	pgx "github.com/jackc/pgx/v4"
)

//go:embed sql/migrations/*.sql
var migrations embed.FS

var m *migrator.Migrator

// MigrateDb upgrades database with all migrations
func (pge *PgEngine) MigrateDb(ctx context.Context) error {
	if err := pge.initMigrator(); err != nil {
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
	return m.NeedUpgrade(ctx, conn.Conn())
}

// ExecuteMigrationScript executes the migration script specified by fname within transaction tx
func ExecuteMigrationScript(ctx context.Context, tx pgx.Tx, fname string) error {
	sql, err := migrations.ReadFile("sql/migrations/" + fname)
	if err != nil {
		return err
	}
	if len(sql) == 0 {
		return errors.New("Empty migration script")
	}
	_, err = tx.Exec(ctx, string(sql))
	return err
}

func (pge *PgEngine) initMigrator() error {
	if m != nil {
		return nil
	}
	var err error
	m, err = migrator.New(
		migrator.TableName("timetable.migration"),
		migrator.SetNotice(func(s string) {
			pge.l.Info(s)
		}),
		migrator.Migrations(
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
			// &migrator.Migration{
			// 	Name: "000XX Short description of a migration",
			// 	Func: func(ctx context.Context, tx pgx.Tx) error {
			// 		return executeMigrationScript(ctx, tx, "000XX.sql")
			// 	},
			// },
			// adding new migration here, update "timetable"."migrations" in "sql/ddl.sql"
		),
	)
	if err != nil {
		pge.l.WithError(err).Error("Cannot initialize migration")
	}
	return err
}
