package pgengine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	pgx "github.com/jackc/pgx/v4"
	"github.com/pashagolub/pgxmock"
	"github.com/stretchr/testify/assert"
)

// func TestInitAndTestMock(t *testing.T) {
// 	initmockdb(t)
// 	pgengine.OpenDB = func(driverName string, dataSourceName string) (*sql.DB, error) {
// 		return db, nil
// 	}
// 	defer mockPool.Close()

// 	t.Run("Check bootstrap if everything fine", func(t *testing.T) {
// 		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
// 		defer cancel()
// 		mockPool.ExpectPing()
// 		mockPool.ExpectQuery("SELECT EXISTS").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
// 		assert.True(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
// 	})

// 	t.Run("Check bootstrap if ping failed", func(t *testing.T) {
// 		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
// 		defer cancel()
// 		mockPool.ExpectPing().WillReturnError(errors.New("ping error"))
// 		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
// 	})

// 	t.Run("Check bootstrap if executeSchemaScripts failed", func(t *testing.T) {
// 		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
// 		defer cancel()
// 		mockPool.ExpectPing()
// 		mockPool.ExpectQuery("SELECT EXISTS").WillReturnError(errors.New("internal error"))
// 		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
// 	})

// 	t.Run("Check bootstrap if startup file doesn't exist", func(t *testing.T) {
// 		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
// 		defer cancel()
// 		mockPool.ExpectPing()
// 		mockPool.ExpectQuery("SELECT EXISTS").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
// 		cmdOpts.File = "foo"
// 		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
// 	})

// 	pgengine.OpenDB = sql.Open

// 	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
// }

func TestExecuteSchemaScripts(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.PgEngine{ConfigDb: mockPool}

	t.Run("Check schema scripts if error returned for SELECT EXISTS", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mockPool.ExpectQuery("SELECT EXISTS").WillReturnError(errors.New("expected"))
		assert.Error(t, mockpge.ExecuteSchemaScripts(ctx))
	})

	t.Run("Check schema scripts if error returned", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mockPool.ExpectQuery("SELECT EXISTS").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectExec("CREATE SCHEMA timetable").WillReturnError(errors.New("expected"))
		mockPool.ExpectExec("DROP SCHEMA IF EXISTS timetable CASCADE").WillReturnError(errors.New("expected"))
		assert.Error(t, mockpge.ExecuteSchemaScripts(ctx))
	})

	t.Run("Check schema scripts if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mockPool.ExpectQuery("SELECT EXISTS").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		for i := 0; i < 4; i++ {
			mockPool.ExpectExec(".*").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		}
		assert.NoError(t, mockpge.ExecuteSchemaScripts(ctx))
	})
}

func TestExecuteCustomScripts(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.PgEngine{ConfigDb: mockPool}
	t.Run("Check ExecuteCustomScripts for non-existent file", func(t *testing.T) {
		assert.Error(t, mockpge.ExecuteCustomScripts(context.Background(), "foo.bar"))
	})

	t.Run("Check ExecuteCustomScripts if error returned", func(t *testing.T) {
		mockPool.ExpectExec("WITH").WillReturnError(errors.New("expected"))
		assert.Error(t, mockpge.ExecuteCustomScripts(context.Background(), "../../samples/basic.sql"))
	})

	t.Run("Check ExecuteCustomScripts if everything fine", func(t *testing.T) {
		mockPool.ExpectExec("WITH").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		assert.NoError(t, mockpge.ExecuteCustomScripts(context.Background(), "../../samples/basic.sql"))
	})
}

func TestReconnectAndFixLeftovers(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.PgEngine{ConfigDb: mockPool}
	t.Run("Check ReconnectAndFixLeftovers if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mockPool.ExpectPing()
		mockPool.ExpectExec("INSERT INTO timetable\\.run_status").WillReturnResult(pgxmock.NewResult("EXECUTE", 0))
		assert.True(t, mockpge.ReconnectAndFixLeftovers(ctx))
	})

	t.Run("Check ReconnectAndFixLeftovers if error returned", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), (pgengine.WaitTime+2)*time.Second)
		defer cancel()
		mockPool.ExpectPing().WillReturnError(errors.New("expected"))
		mockPool.ExpectPing().WillDelayFor(pgengine.WaitTime * time.Second * 2)
		assert.False(t, mockpge.ReconnectAndFixLeftovers(ctx))
	})
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

func TestLogger(t *testing.T) {
	l := pgengine.Logger{}
	for level := pgx.LogLevelNone; level <= pgx.LogLevelTrace; level++ {
		l.Log(context.Background(), pgx.LogLevel(level), "", nil)
	}
}

func TestFinalizeConnection(t *testing.T) {
	initmockdb(t)
	mockpge := pgengine.PgEngine{ConfigDb: mockPool}
	mockPool.ExpectClose().WillReturnError(errors.New("expected"))
	mockpge.Finalize()
}
