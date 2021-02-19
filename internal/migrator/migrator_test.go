package migrator_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/cybertec-postgresql/pg_timetable/internal/migrator"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	pgx "github.com/jackc/pgx/v4"
	"github.com/stretchr/testify/assert"
)

var migrations = []interface{}{
	&migrator.Migration{
		Name: "Using tx, encapsulate two queries",
		Func: func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "CREATE TABLE foo (id INT PRIMARY KEY)"); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO foo (id) VALUES (1)"); err != nil {
				return err
			}
			return nil
		},
	},
	&migrator.MigrationNoTx{
		Name: "Using db, execute one query",
		Func: func(ctx context.Context, db *pgx.Conn) error {
			if _, err := db.Exec(ctx, "INSERT INTO foo (id) VALUES (2)"); err != nil {
				return err
			}
			return nil
		},
	},
	&migrator.Migration{
		Name: "Using tx, encapsulate two queries",
		Func: func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "CREATE TABLE bar (id INT PRIMARY KEY)"); err != nil {
				return err
			}
			return nil
		},
	},
}

func migrateTest() error {
	migrator, err := migrator.New(migrator.Migrations(migrations...))
	if err != nil {
		return err
	}

	// Migrate up
	ctx := context.Background()
	pgengine.InitAndTestConfigDBConnection(ctx, *cmdparser.NewCmdOptions("migrator_unit_test"))
	_, _ = pgengine.ConfigDb.Exec(ctx, "DROP TABLE IF EXISTS foo, bar, baz")
	db, err := pgengine.ConfigDb.Acquire(ctx)
	if err != nil {
		return err
	}
	defer db.Release()
	if err := migrator.Migrate(ctx, db.Conn()); err != nil {
		return err
	}

	return nil
}

func mustMigrator(migrator *migrator.Migrator, err error) *migrator.Migrator {
	if err != nil {
		panic(err)
	}
	return migrator
}

func TestPostgres(t *testing.T) {
	if err := migrateTest(); err != nil {
		t.Fatal(err)
	}
}

func TestBadMigrations(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdparser.NewCmdOptions("migrator_unit_test"))
	ctx := context.Background()
	db, err := pgengine.ConfigDb.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Release()

	var migrators = []struct {
		name  string
		input *migrator.Migrator
		want  error
	}{
		{
			name: "bad tx migration",
			input: mustMigrator(migrator.New(migrator.Migrations(&migrator.Migration{
				Name: "bad tx migration",
				Func: func(ctx context.Context, tx pgx.Tx) error {
					if _, err := tx.Exec(ctx, "FAIL FAST"); err != nil {
						return err
					}
					return nil
				},
			}))),
		},
		{
			name: "bad db migration",
			input: mustMigrator(migrator.New(migrator.Migrations(&migrator.MigrationNoTx{
				Name: "bad db migration",
				Func: func(ctx context.Context, db *pgx.Conn) error {
					if _, err := db.Exec(ctx, "FAIL FAST"); err != nil {
						return err
					}
					return nil
				},
			}))),
		},
	}

	for _, tt := range migrators {
		_, err := db.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tt.input.TableName))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Migrate(context.Background(), db.Conn())
			if err != nil && !strings.Contains(err.Error(), "syntax error") {
				t.Fatal(err)
			}
		})
	}
}

func TestBadMigrationNumber(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdparser.NewCmdOptions("migrator_unit_test"))
	ctx := context.Background()
	db, err := pgengine.ConfigDb.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Release()
	migrator := mustMigrator(migrator.New(migrator.Migrations(
		&migrator.Migration{
			Name: "bad migration number",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "CREATE TABLE bar (id INT PRIMARY KEY)"); err != nil {
					return err
				}
				return nil
			},
		},
	)))
	if err := migrator.Migrate(context.Background(), db.Conn()); err == nil {
		t.Fatalf("BAD MIGRATION NUMBER should fail: %v", err)
	}
}

func TestPending(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdparser.NewCmdOptions("migrator_unit_test"))
	ctx := context.Background()
	db, err := pgengine.ConfigDb.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Release()
	migrator := mustMigrator(migrator.New(migrator.Migrations(
		&migrator.Migration{
			Name: "Using tx, create baz table",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "CREATE TABLE baz (id INT PRIMARY KEY)"); err != nil {
					return err
				}
				return nil
			},
		},
	)))
	pending, _, err := migrator.Pending(context.Background(), db.Conn())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending migrations should be 1, got %d", len(pending))
	}
}

func TestMigratorConstructor(t *testing.T) {
	_, err := migrator.New() //migrator.Migrations(migrations...)
	assert.Error(t, err, "Should throw error when migrations are empty")

	_, err = migrator.New(migrator.Migrations(struct{ Foo string }{Foo: "bar"}))
	assert.Error(t, err, "Should throw error for unknown migration type")
}
