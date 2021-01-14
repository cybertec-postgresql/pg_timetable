package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

func TestAsyncChains(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	assert.NoError(t, err)
	pgengine.ConfigDb = sqlx.NewDb(db, "sqlmock")
	n := &pgconn.Notification{Payload: `{"ConfigID": 24, "Command": "START"}`}

	//add correct chain
	pgengine.NotificationHandler(&pgconn.PgConn{}, n)
	mock.ExpectQuery("SELECT.+chain_execution_config").
		WillReturnRows(sqlmock.NewRows([]string{"chain_execution_config", "chain_id", "chain_name",
			"self_destruct", "exclusive_execution", "max_instances"}).
			AddRow(24, 24, "foo", false, false, 16))
	if pgengine.VerboseLogLevel {
		mock.ExpectExec("INSERT.+log").WillReturnResult(sqlmock.NewResult(0, 1))
	}
	//add incorrect chaing
	pgengine.NotificationHandler(&pgconn.PgConn{}, n)
	mock.ExpectQuery("SELECT.+chain_execution_config").WillReturnError(errors.New("error"))
	mock.ExpectExec("INSERT.+log").WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestChainWorker(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	assert.NoError(t, err)
	pgengine.ConfigDb = sqlx.NewDb(db, "sqlmock")
	pgengine.VerboseLogLevel = false
	chains := make(chan Chain, workersNumber)

	t.Run("Check chainWorker if context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		chains <- Chain{}
		chainWorker(ctx, chains)
	})

	t.Run("Check chainWorker if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mock.ExpectQuery("SELECT count").WillReturnError(sql.ErrNoRows)
		mock.ExpectBegin().WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("DELETE").WillReturnResult(sqlmock.NewResult(0, 1))
		chains <- Chain{SelfDestruct: true}
		chainWorker(ctx, chains)
	})

	t.Run("Check chainWorker if cannot proceed with chain execution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), (pgengine.WaitTime+2)*time.Second)
		defer cancel()
		mock.ExpectQuery("SELECT count").WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT count").WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(sqlmock.NewResult(0, 1))
		chains <- Chain{}
		chainWorker(ctx, chains)
	})
}
