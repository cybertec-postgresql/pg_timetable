package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
)

func TestSchedulerExclusiveLocking(*testing.T) {
	sch := &Scheduler{exclusiveMutex: sync.RWMutex{}}
	sch.Lock(true)
	sch.Unlock(true)
	sch.Lock(false)
	sch.Unlock(false)
}

func TestAsyncChains(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "scheduler_unit_test")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}))
	n1 := &pgconn.Notification{Payload: `{"ConfigID": 1, "Command": "START"}`}
	n2 := &pgconn.Notification{Payload: `{"ConfigID": 2, "Command": "START"}`}
	ns := &pgconn.Notification{Payload: `{"ConfigID": 24, "Command": "STOP"}`}

	//add correct chain
	pge.NotificationHandler(&pgconn.PgConn{}, n1)
	pge.NotificationHandler(&pgconn.PgConn{}, ns)
	mock.ExpectQuery("SELECT.+chain_id").
		WillReturnRows(pgxmock.NewRows([]string{"chain_id", "task_id", "chain_name",
			"self_destruct", "exclusive_execution", "max_instances"}).
			AddRow(24, 24, "foo", false, false, 16))
	if pge.Verbose() {
		mock.ExpectExec("INSERT.+log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	sch.retrieveAsyncChainsAndRun(ctx)
	//add incorrect chain
	pge.NotificationHandler(&pgconn.PgConn{}, n2)
	mock.ExpectQuery("SELECT.+chain_id").WillReturnError(errors.New("error"))
	mock.ExpectExec("INSERT.+log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	sch.retrieveAsyncChainsAndRun(ctx)
}

func TestChainWorker(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "-c", "scheduler_unit_test", "--password=somestrong")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic"}))
	chains := make(chan Chain, 16)

	t.Run("Check chainWorker if context cancelled", func(*testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		chains <- Chain{}
		sch.chainWorker(ctx, chains)
	})

	t.Run("Check chainWorker if everything fine", func(*testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mock.ExpectQuery("SELECT count").WillReturnError(pgx.ErrNoRows)
		mock.ExpectBegin().WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec("DELETE").WillReturnResult(pgxmock.NewResult("DELETE", 1))
		chains <- Chain{SelfDestruct: true}
		sch.chainWorker(ctx, chains)
	})

	t.Run("Check chainWorker if cannot proceed with chain execution", func(*testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime+2)
		defer cancel()
		mock.ExpectQuery("SELECT count").WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectQuery("SELECT count").WillReturnError(errors.New("expected"))
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("INSERT", 1))
		chains <- Chain{}
		sch.chainWorker(ctx, chains)
	})
}

func TestExecuteChain(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "-c", "scheduler_unit_test", "--password=somestrong")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}))

	sch.executeChain(t.Context(), Chain{Timeout: 1})
}

func TestExecuteChainElement(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "-c", "scheduler_unit_test", "--password=somestrong")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}))

	mock.ExpectQuery("SELECT").WillReturnRows(pgxmock.NewRows([]string{"value"}).AddRow("foo"))
	_ = sch.executeTask(t.Context(), mock, &pgengine.ChainTask{Timeout: 1})
}

func TestExecuteOnErrorHandler(t *testing.T) {
	c := Chain{ChainID: 42, OnError: "FOO"}
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "-c", "scheduler_unit_test", "--password=somestrong")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}))

	t.Run("check error handler if everything is fine", func(t *testing.T) {
		mock.ExpectExec("FOO").WillReturnResult(pgxmock.NewResult("FOO", 1))
		sch.executeOnErrorHandler(context.Background(), c)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("check error handler if context cancelled", func(*testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		sch.executeOnErrorHandler(ctx, c)
	})

	t.Run("check error handler if error", func(*testing.T) {
		mock.ExpectExec("FOO").WillReturnError(errors.New("Syntax error near FOO"))
		sch.executeOnErrorHandler(context.Background(), c)
	})

	assert.NoError(t, mock.ExpectationsWereMet())
}
