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
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	pge.Verbose = false

	t.Run("Check DeleteChainConfig if everyhing fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectExec("DELETE FROM timetable\\.chain_execution_config").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		assert.True(t, pge.DeleteChainConfig(ctx, 0))
	})

	t.Run("Check DeleteChainConfig if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectExec("DELETE FROM timetable\\.chain_execution_config").WillReturnError(errors.New("error"))
		assert.False(t, pge.DeleteChainConfig(ctx, 0))
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestFixSchedulerCrash(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	pge.Verbose = false

	t.Run("Check FixSchedulerCrash if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectExec(`SELECT timetable\.health_check`).WillReturnError(errors.New("error"))
		pge.FixSchedulerCrash(ctx)
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestCanProceedChainExecution(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	pge.Verbose = false

	t.Run("Check CanProceedChainExecution if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectQuery("SELECT count").WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
		assert.False(t, pge.CanProceedChainExecution(ctx, 0, 0), "Proc count is less than maxinstances")
	})

	t.Run("Check CanProceedChainExecution gets ErrNoRows", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectQuery("SELECT count").WillReturnError(pgx.ErrNoRows)
		assert.True(t, pge.CanProceedChainExecution(ctx, 0, 0))
	})

	t.Run("Check CanProceedChainExecution if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectQuery("SELECT count").WillReturnError(errors.New("error"))
		assert.False(t, pge.CanProceedChainExecution(ctx, 0, 0))
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestInsertChainRunStatus(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	pge.Verbose = false

	t.Run("Check InsertChainRunStatus if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectQuery("INSERT INTO timetable\\.run_status").WillReturnError(errors.New("error"))
		pge.InsertChainRunStatus(ctx, 0, 0)
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestUpdateChainRunStatus(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()
	pge.Verbose = false

	t.Run("Check UpdateChainRunStatus if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mockPool.ExpectExec("INSERT INTO timetable\\.run_status").WillReturnError(errors.New("error"))
		pge.UpdateChainRunStatus(ctx, &pgengine.ChainElementExecution{}, 0, "STATUS")
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestSelectChain(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	mockPool.ExpectExec("SELECT.+chain_execution_config").WillReturnError(errors.New("error"))
	assert.Error(t, pge.SelectChain(context.Background(), struct{}{}, 42))
}

func TestIsAlive(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()
	mockPool.ExpectPing()
	assert.True(t, pge.IsAlive())
}

func TestLogChainElementExecution(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	t.Run("Check LogChainElementExecution if sql fails", func(t *testing.T) {
		mockPool.ExpectExec("INSERT INTO .*execution_log").WillReturnError(errors.New("error"))
		pge.LogChainElementExecution(context.TODO(), &pgengine.ChainElementExecution{}, 0, "STATUS")
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}
