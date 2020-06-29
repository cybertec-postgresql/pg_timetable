package pgengine_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/stretchr/testify/assert"
)

func TestLogToDb(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = xdb
	defer db.Close()

	t.Run("Check LogToDB in terse mode", func(t *testing.T) {
		pgengine.VerboseLogLevel = false
		pgengine.LogToDB(context.TODO(), "DEBUG", "Test DEBUG message")

	})

	t.Run("Check LogToDB in verbose mode", func(t *testing.T) {
		pgengine.VerboseLogLevel = true
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnError(sql.ErrConnDone)
		pgengine.LogToDB(context.TODO(), "DEBUG", "Test DEBUG message")
	})

	assert.NoError(t, mock.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestLogChainElementExecution(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = xdb
	defer db.Close()

	t.Run("Check LogChainElementExecution if sql fails", func(t *testing.T) {
		mock.ExpectExec("INSERT INTO timetable\\.execution_log").WillReturnError(errors.New("error"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(sqlmock.NewResult(0, 1))
		pgengine.LogChainElementExecution(context.TODO(), &pgengine.ChainElementExecution{}, 0, "STATUS")
	})

	assert.NoError(t, mock.ExpectationsWereMet(), "there were unfulfilled expectations")
}
