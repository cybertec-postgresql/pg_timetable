package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgconn"
	pgx "github.com/jackc/pgx/v4"
	"github.com/pashagolub/pgxmock"
	"github.com/stretchr/testify/assert"
)

func TestAsyncChains(t *testing.T) {
	mock, err := pgxmock.NewPool(pgxmock.MonitorPingsOption(true))
	assert.NoError(t, err)
	pgengine.ConfigDb = mock
	n := &pgconn.Notification{Payload: `{"ConfigID": 24, "Command": "START"}`}

	//add correct chain
	pgengine.NotificationHandler(&pgconn.PgConn{}, n)
	mock.ExpectQuery("SELECT.+chain_execution_config").
		WillReturnRows(pgxmock.NewRows([]string{"chain_execution_config", "chain_id", "chain_name",
			"self_destruct", "exclusive_execution", "max_instances"}).
			AddRow(24, 24, "foo", false, false, 16))
	if pgengine.VerboseLogLevel {
		mock.ExpectExec("INSERT.+log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
	}
	//add incorrect chaing
	pgengine.NotificationHandler(&pgconn.PgConn{}, n)
	mock.ExpectQuery("SELECT.+chain_execution_config").WillReturnError(errors.New("error"))
	mock.ExpectExec("INSERT.+log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
}

func TestChainWorker(t *testing.T) {
	mock, err := pgxmock.NewPool() //pgxmock.MonitorPingsOption(true)
	assert.NoError(t, err)
	pgengine.ConfigDb = mock
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
		mock.ExpectQuery("SELECT count").WillReturnError(pgx.ErrNoRows)
		mock.ExpectBegin().WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec("DELETE").WillReturnResult(pgxmock.NewResult("DELETE", 1))
		chains <- Chain{SelfDestruct: true}
		chainWorker(ctx, chains)
	})

	t.Run("Check chainWorker if cannot proceed with chain execution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), (pgengine.WaitTime+2)*time.Second)
		defer cancel()
		mock.ExpectQuery("SELECT count").WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectQuery("SELECT count").WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		chains <- Chain{}
		chainWorker(ctx, chains)
	})
}
