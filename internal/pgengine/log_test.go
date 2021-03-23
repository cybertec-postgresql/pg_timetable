package pgengine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/pashagolub/pgxmock"
	"github.com/stretchr/testify/assert"
)

func TestLogError(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	mockPool.ExpectExec(`INSERT INTO timetable.*`).WillReturnError(errors.New("error"))
	pge.LogToDB(context.TODO(), "LOG", "Should fail")
}

func TestLogToDb(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	t.Run("Check LogToDB in terse mode", func(t *testing.T) {
		pge.Verbose = false
		pge.LogToDB(context.TODO(), "DEBUG", "Test DEBUG message")
	})

	t.Run("Check LogToDB in verbose mode", func(t *testing.T) {
		pge.Verbose = true
		mockPool.ExpectExec("INSERT INTO timetable.*").WillReturnError(errors.New("error"))
		pge.LogToDB(context.TODO(), "DEBUG", "Test DEBUG message")
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestLogChainElementExecution(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	defer mockPool.Close()

	t.Run("Check LogChainElementExecution if sql fails", func(t *testing.T) {
		mockPool.ExpectExec("INSERT INTO .*execution_log").WillReturnError(errors.New("error"))
		mockPool.ExpectExec("INSERT INTO .*log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		pge.LogChainElementExecution(context.TODO(), &pgengine.ChainElementExecution{}, 0, "STATUS")
	})

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}
