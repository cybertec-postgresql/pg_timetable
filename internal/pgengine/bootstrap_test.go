package pgengine_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	pgx "github.com/jackc/pgx/v4"
	"github.com/stretchr/testify/assert"
)

func TestInitAndTestMock(t *testing.T) {
	initmockdb(t)
	pgengine.OpenDB = func(driverName string, dataSourceName string) (*sql.DB, error) {
		return db, nil
	}
	defer db.Close()

	t.Run("Check bootstrap if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectPing()
		mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		assert.True(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
	})

	t.Run("Check bootstrap if ping failed", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectPing().WillReturnError(errors.New("ping error"))
		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
	})

	t.Run("Check bootstrap if executeSchemaScripts failed", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectPing()
		mock.ExpectQuery("SELECT EXISTS").WillReturnError(errors.New("internal error"))
		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
	})

	t.Run("Check bootstrap if startup file doesn't exist", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectPing()
		mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		cmdOpts.File = "foo"
		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
	})

	pgengine.OpenDB = sql.Open

	assert.NoError(t, mock.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestExecuteSchemaScripts(t *testing.T) {
	initmockdb(t)
	defer db.Close()
	pgengine.ConfigDb = xdb

	t.Run("Check schema scripts if error returned for SELECT EXISTS", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectQuery("SELECT EXISTS").WillReturnError(errors.New("expected"))
		assert.False(t, pgengine.ExecuteSchemaScripts(ctx))
	})

	t.Run("Check schema scripts if error returned", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
		mock.ExpectExec("CREATE SCHEMA timetable").WillReturnError(errors.New("expected"))
		mock.ExpectExec("DROP SCHEMA IF EXISTS timetable CASCADE").WillReturnError(errors.New("expected"))
		assert.False(t, pgengine.ExecuteSchemaScripts(ctx))
	})

	t.Run("Check schema scripts if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
		for i := 0; i < 4; i++ {
			mock.ExpectExec(".*").WillReturnResult(sqlmock.NewResult(0, 1))
		}
		assert.True(t, pgengine.ExecuteSchemaScripts(ctx))
	})
}

func TestExecuteCustomScripts(t *testing.T) {
	initmockdb(t)
	defer db.Close()
	pgengine.ConfigDb = xdb

	t.Run("Check ExecuteCustomScripts for non-existent file", func(t *testing.T) {
		assert.False(t, pgengine.ExecuteCustomScripts(context.Background(), "foo.bar"))
	})

	t.Run("Check ExecuteCustomScripts if error returned", func(t *testing.T) {
		mock.ExpectExec("WITH").WillReturnError(errors.New("expected"))
		assert.False(t, pgengine.ExecuteCustomScripts(context.Background(), "../../samples/basic.sql"))
	})

	t.Run("Check ExecuteCustomScripts if everything fine", func(t *testing.T) {
		mock.ExpectExec("WITH").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(sqlmock.NewResult(0, 1))
		assert.True(t, pgengine.ExecuteCustomScripts(context.Background(), "../../samples/basic.sql"))
	})
}

func TestReconnectAndFixLeftovers(t *testing.T) {
	initmockdb(t)
	defer db.Close()
	pgengine.ConfigDb = xdb

	t.Run("Check ReconnectAndFixLeftovers if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectPing()
		mock.ExpectExec("INSERT INTO timetable\\.run_status").WillReturnResult(sqlmock.NewResult(0, 0))
		assert.True(t, pgengine.ReconnectDbAndFixLeftovers(ctx))
	})

	t.Run("Check ReconnectAndFixLeftovers if error returned", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mock.ExpectPing().WillReturnError(errors.New("expected"))
		mock.ExpectPing().WillDelayFor(pgengine.WaitTime * time.Second * 2)
		assert.False(t, pgengine.ReconnectDbAndFixLeftovers(ctx))
	})
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLogger(t *testing.T) {
	l := pgengine.Logger{}
	for level := pgx.LogLevelNone; level <= pgx.LogLevelTrace; level++ {
		l.Log(context.Background(), pgx.LogLevel(level), "", nil)
	}
}

func TestFinalizeConnection(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = xdb
	mock.ExpectClose().WillReturnError(errors.New("expected"))
	pgengine.FinalizeConfigDBConnection()
}
