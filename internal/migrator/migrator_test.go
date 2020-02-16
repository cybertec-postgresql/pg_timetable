package migrator_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/migrator"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

var migrations = []interface{}{
	&migrator.Migration{
		Name: "Using tx, encapsulate two queries",
		Func: func(tx *sql.Tx) error {
			if _, err := tx.Exec("CREATE TABLE foo (id INT PRIMARY KEY)"); err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO foo (id) VALUES (1)"); err != nil {
				return err
			}
			return nil
		},
	},
	&migrator.MigrationNoTx{
		Name: "Using db, execute one query",
		Func: func(db *sql.DB) error {
			if _, err := db.Exec("INSERT INTO foo (id) VALUES (2)"); err != nil {
				return err
			}
			return nil
		},
	},
	&migrator.Migration{
		Name: "Using tx, encapsulate two queries",
		Func: func(tx *sql.Tx) error {
			if _, err := tx.Exec("CREATE TABLE bar (id INT PRIMARY KEY)"); err != nil {
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
	pgengine.InitAndTestConfigDBConnection([]string{})
	pgengine.ConfigDb.MustExec("DROP TABLE IF EXISTS foo, bar, baz")
	if err := migrator.Migrate(pgengine.ConfigDb.DB); err != nil {
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

func TestDatabaseNotFound(t *testing.T) {
	migrator, err := migrator.New(migrator.Migrations(&migrator.Migration{}))
	if err != nil {
		t.Fatal(err)
	}
	db, _ := sql.Open("postgres", "")
	if err := migrator.Migrate(db); err == nil {
		t.Fatal(err)
	}
}

func TestBadMigrations(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection([]string{})
	db := pgengine.ConfigDb.DB

	var migrators = []struct {
		name  string
		input *migrator.Migrator
		want  error
	}{
		{
			name: "bad tx migration",
			input: mustMigrator(migrator.New(migrator.Migrations(&migrator.Migration{
				Name: "bad tx migration",
				Func: func(tx *sql.Tx) error {
					if _, err := tx.Exec("FAIL FAST"); err != nil {
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
				Func: func(db *sql.DB) error {
					if _, err := db.Exec("FAIL FAST"); err != nil {
						return err
					}
					return nil
				},
			}))),
		},
	}

	for _, tt := range migrators {
		_, err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tt.input.TableName))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Migrate(db)
			if err != nil && !strings.Contains(err.Error(), "pq: syntax error") {
				t.Fatal(err)
			}
		})
	}
}

func TestBadMigrationNumber(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection([]string{})
	db := pgengine.ConfigDb.DB
	migrator := mustMigrator(migrator.New(migrator.Migrations(
		&migrator.Migration{
			Name: "bad migration number",
			Func: func(tx *sql.Tx) error {
				if _, err := tx.Exec("CREATE TABLE bar (id INT PRIMARY KEY)"); err != nil {
					return err
				}
				return nil
			},
		},
	)))
	if err := migrator.Migrate(db); err == nil {
		t.Fatalf("BAD MIGRATION NUMBER should fail: %v", err)
	}
}

func TestPending(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection([]string{})
	db := pgengine.ConfigDb.DB
	migrator := mustMigrator(migrator.New(migrator.Migrations(
		&migrator.Migration{
			Name: "Using tx, create baz table",
			Func: func(tx *sql.Tx) error {
				if _, err := tx.Exec("CREATE TABLE baz (id INT PRIMARY KEY)"); err != nil {
					return err
				}
				return nil
			},
		},
	)))
	pending, err := migrator.Pending(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending migrations should be 1, got %d", len(pending))
	}
}
