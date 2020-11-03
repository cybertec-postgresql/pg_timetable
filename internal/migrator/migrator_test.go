package migrator_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/cybertec-postgresql/pg_timetable/internal/migrator"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/stretchr/testify/assert"
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
		Func: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, "INSERT INTO foo (id) VALUES (2)"); err != nil {
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
	pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdparser.NewCmdOptions("migrator_unit_test"))
	pgengine.ConfigDb.MustExec("DROP TABLE IF EXISTS foo, bar, baz")
	if err := migrator.Migrate(context.Background(), pgengine.ConfigDb.DB); err != nil {
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
	m, err := migrator.New(migrator.Migrations(&migrator.Migration{}))
	if err != nil {
		t.Fatal(err)
	}
	db, _ := sql.Open("pgx", "foobarbaz")
	if err := m.Migrate(context.Background(), db); err == nil {
		t.Fatal(err)
	}
}

func TestBadMigrations(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdparser.NewCmdOptions("migrator_unit_test"))
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
				Func: func(ctx context.Context, db *sql.DB) error {
					if _, err := db.ExecContext(ctx, "FAIL FAST"); err != nil {
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
			err := tt.input.Migrate(context.Background(), db)
			if err != nil && !strings.Contains(err.Error(), "syntax error") {
				t.Fatal(err)
			}
		})
	}
}

func TestBadMigrationNumber(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdparser.NewCmdOptions("migrator_unit_test"))
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
	if err := migrator.Migrate(context.Background(), db); err == nil {
		t.Fatalf("BAD MIGRATION NUMBER should fail: %v", err)
	}
}

func TestPending(t *testing.T) {
	pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdparser.NewCmdOptions("migrator_unit_test"))
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
	pending, _, err := migrator.Pending(context.Background(), db)
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

func TestTableExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	m, err := migrator.New(migrator.Migrations(migrations...))
	assert.NoError(t, err)
	assert.NotNil(t, m)

	sqlresults := []struct {
		testname     string
		tableexists  bool
		appliedcount int
		needupgrade  bool
		tableerr     error
		counterr     error
	}{
		{
			testname:     "table exists and no migrations applied",
			tableexists:  true,
			appliedcount: 0,
			needupgrade:  true,
			tableerr:     nil,
			counterr:     nil,
		},
		{
			testname:     "table exists and a lot of migrations applied",
			tableexists:  true,
			appliedcount: math.MaxInt32,
			needupgrade:  false,
			tableerr:     nil,
			counterr:     nil,
		},
		{
			testname:     "error occurred during count query",
			tableexists:  true,
			appliedcount: 0,
			needupgrade:  false,
			tableerr:     nil,
			counterr:     errors.New("internal error"),
		},
		{
			testname:     "error occurred during table exists query",
			tableexists:  false,
			appliedcount: 0,
			needupgrade:  true,
			tableerr:     errors.New("internal error"),
			counterr:     nil,
		},
	}
	var expectederr error
	for _, res := range sqlresults {
		if q := mock.ExpectQuery("SELECT to_regclass"); res.tableerr != nil {
			q.WillReturnError(res.tableerr)
			expectederr = res.tableerr
		} else {
			q.WillReturnRows(sqlmock.NewRows([]string{"to_regclass"}).AddRow(res.tableexists))
		}
		if q := mock.ExpectQuery("SELECT count"); res.counterr != nil {
			q.WillReturnError(res.counterr)
			expectederr = res.counterr
		} else {
			q.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(res.appliedcount))
		}
		need, err := m.NeedUpgrade(context.Background(), db)
		assert.Equal(t, expectederr, err, "NeedUpgrade test failed: ", res.testname)
		assert.Equal(t, res.needupgrade, need, "NeedUpgrade incorrect return: ", res.testname)
	}
}

func TestMigrateExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	m, err := migrator.New(migrator.Migrations(migrations...))
	assert.NoError(t, err)
	assert.NotNil(t, m)

	expectederr := errors.New("internal error")

	mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT count").WillReturnError(expectederr)

	err = m.Migrate(context.Background(), db)
	assert.Equal(t, expectederr, err, "Migrate test failed: ", err)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestMigrateNoTxError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	m, err := migrator.New(migrator.Migrations(&migrator.MigrationNoTx{Func: func(context.Context, *sql.DB) error { return nil }}))
	assert.NoError(t, err)
	assert.NotNil(t, m)

	expectederr := errors.New("internal error")

	mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec("INSERT").WillReturnError(expectederr)

	err = m.Migrate(context.Background(), db)
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	assert.Equal(t, expectederr, err, "MigrateNoTxError test failed: ", err)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestMigrateTxError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	m, err := migrator.New(migrator.Migrations(&migrator.Migration{Func: func(*sql.Tx) error { return nil }}))
	assert.NoError(t, err)
	assert.NotNil(t, m)

	expectederr := errors.New("internal tx error")
	mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin().WillReturnError(expectederr)
	err = m.Migrate(context.Background(), db)
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	assert.Equal(t, expectederr, err, "MigrateTxError test failed: ", err)

	expectederr = errors.New("internal tx error")
	mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT").WillReturnError(expectederr)
	err = m.Migrate(context.Background(), db)
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	assert.Equal(t, expectederr, err, "MigrateTxError test failed: ", err)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}
