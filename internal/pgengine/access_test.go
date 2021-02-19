package pgengine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgx/v4"
	"github.com/pashagolub/pgxmock"
	"github.com/stretchr/testify/assert"
)

func TestDeleteChainConfig(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = mockPool
	defer mockPool.Close()

	pgengine.VerboseLogLevel = false

	t.Run("Check DeleteChainConfig if everyhing fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		mockPool.ExpectExec("DELETE FROM timetable\\.chain_execution_config").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		assert.True(t, pgengine.DeleteChainConfig(ctx, 0))
	})

	t.Run("Check DeleteChainConfig if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		mockPool.ExpectExec("DELETE FROM timetable\\.chain_execution_config").WillReturnError(errors.New("error"))
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		assert.False(t, pgengine.DeleteChainConfig(ctx, 0))
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestFixSchedulerCrash(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = mockPool
	defer mockPool.Close()

	pgengine.VerboseLogLevel = false

	t.Run("Check FixSchedulerCrash if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectExec("INSERT INTO timetable\\.run_status").WillReturnError(errors.New("error"))
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		pgengine.FixSchedulerCrash(ctx)
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestCanProceedChainExecution(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = mockPool
	defer mockPool.Close()

	pgengine.VerboseLogLevel = false

	t.Run("Check CanProceedChainExecution if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectQuery("SELECT count").WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
		assert.False(t, pgengine.CanProceedChainExecution(ctx, 0, 0), "Proc count is less than maxinstances")
	})

	t.Run("Check CanProceedChainExecution gets ErrNoRows", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectQuery("SELECT count").WillReturnError(pgx.ErrNoRows)
		assert.True(t, pgengine.CanProceedChainExecution(ctx, 0, 0))
	})

	t.Run("Check CanProceedChainExecution if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectQuery("SELECT count").WillReturnError(errors.New("error"))
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		assert.False(t, pgengine.CanProceedChainExecution(ctx, 0, 0))
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestInsertChainRunStatus(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = mockPool
	defer mockPool.Close()

	pgengine.VerboseLogLevel = false

	t.Run("Check InsertChainRunStatus if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectQuery("INSERT INTO timetable\\.run_status").WillReturnError(errors.New("error"))
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		pgengine.InsertChainRunStatus(ctx, 0, 0)
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestUpdateChainRunStatus(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = mockPool
	defer mockPool.Close()

	pgengine.VerboseLogLevel = false

	t.Run("Check UpdateChainRunStatus if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectExec("INSERT INTO timetable\\.run_status").WillReturnError(errors.New("error"))
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		pgengine.UpdateChainRunStatus(ctx, &pgengine.ChainElementExecution{}, 0, "STATUS")
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestSelectChain(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = mockPool
	defer mockPool.Close()

	mockPool.ExpectExec("SELECT.+chain_execution_config").WillReturnError(errors.New("error"))
	assert.Error(t, pgengine.SelectChain(context.Background(), struct{}{}, 42))
}

func TestIsAlive(t *testing.T) {
	initmockdb(t)
	assert.False(t, pgengine.IsAlive())

	pgengine.ConfigDb = mockPool
	defer mockPool.Close()
	mockPool.ExpectPing()
	assert.True(t, pgengine.IsAlive())
}
