package pgengine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/pashagolub/pgxmock/v2"
	"github.com/stretchr/testify/assert"
)

func TestDeleteChainConfig(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	t.Run("Check DeleteChainConfig if everyhing fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime+2)
		defer cancel()
		mockPool.ExpectExec("DELETE FROM timetable\\.chain").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		assert.True(t, pge.DeleteChainConfig(ctx, 0))
	})

	t.Run("Check DeleteChainConfig if sql fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime+2)
		defer cancel()
		mockPool.ExpectExec("DELETE FROM timetable\\.chain").WillReturnError(errors.New("error"))
		assert.False(t, pge.DeleteChainConfig(ctx, 0))
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestInsertChainRunStatus(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	pge.ClientName = "test_client"
	defer mockPool.Close()

	mockPool.ExpectExec("INSERT INTO timetable\\.active_chain").
		WithArgs(0, pge.ClientName, 1).
		WillReturnError(errors.New("error"))
	pge.InsertChainRunStatus(context.Background(), 0, 1)

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestRemoveChainRunStatus(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	pge.ClientName = "test_client"
	defer mockPool.Close()

	mockPool.ExpectExec("DELETE FROM timetable\\.active_chain").
		WithArgs(0, pge.ClientName).
		WillReturnError(errors.New("error"))
	pge.RemoveChainRunStatus(context.Background(), 0)

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestSelectChains(t *testing.T) {
	var c *[]pgengine.Chain
	var ic *[]pgengine.IntervalChain
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	mockPool.ExpectExec("SELECT.+chain_id").WillReturnError(errors.New("error"))
	assert.Error(t, pge.SelectChains(context.Background(), c))

	mockPool.ExpectExec("SELECT.+chain_id").WillReturnError(errors.New("error"))
	assert.Error(t, pge.SelectRebootChains(context.Background(), c))

	mockPool.ExpectExec("SELECT.+chain_id").WillReturnError(errors.New("error"))
	assert.Error(t, pge.SelectIntervalChains(context.Background(), ic))
}

func TestSelectChain(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	mockPool.ExpectExec("SELECT.+chain_id").WillReturnError(errors.New("error"))
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
		pge.LogChainElementExecution(context.TODO(), &pgengine.ChainTask{}, 0, "STATUS")
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}
