package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/otel"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v6"
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
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())
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
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic"}), otel.NewNoop())
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
		mock.ExpectExec("DELETE FROM timetable\\.active_chain").WillReturnResult(pgxmock.NewResult("DELETE", 1))
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
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())

	sch.executeChain(t.Context(), Chain{Timeout: 1})
}

func TestExecuteChainEarlyReturn(t *testing.T) {
	c := Chain{ChainID: 42, OnError: "FOO"}
	vxidRows := func() *pgxmock.Rows { return pgxmock.NewRows([]string{"vxid"}).AddRow(int64(42)) }

	tests := []struct {
		name    string
		ctx     func() context.Context
		expect  func(mock pgxmock.PgxPoolIface)
		onError bool
	}{
		{
			name: "cannot start transaction",
			ctx:  context.Background,
			expect: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectBegin().WillReturnError(errors.New("query_wait_timeout"))
			},
			onError: true,
		},
		{
			name: "chain stopped before start",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			expect: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectBegin()
			},
		},
		{
			name: "cannot get virtual transaction id",
			ctx:  context.Background,
			expect: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectBegin()
				mock.ExpectQuery("FROM pg_locks").WillReturnError(errors.New("query_wait_timeout"))
				mock.ExpectRollback()
			},
			onError: true,
		},
		{
			name: "failed to retrieve chain elements",
			ctx:  context.Background,
			expect: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectBegin()
				mock.ExpectQuery("FROM pg_locks").WillReturnRows(vxidRows())
				mock.ExpectQuery("FROM timetable\\.task").WithArgs(c.ChainID).WillReturnError(errors.New("query_wait_timeout"))
				mock.ExpectRollback()
			},
			onError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			assert.NoError(t, err)
			pge := pgengine.NewDB(mock, "-c", "scheduler_unit_test")
			sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())

			tc.expect(mock)
			if tc.onError {
				expectOnError(mock)
			} else {
				mock.ExpectBegin() // must stay unconsumed: no on_error for a stopped chain
			}

			sch.executeChain(tc.ctx(), c)
			if tc.onError {
				assert.NoError(t, mock.ExpectationsWereMet())
			} else {
				assert.Error(t, mock.ExpectationsWereMet())
			}
		})
	}
}

func expectOnError(mock pgxmock.PgxPoolIface) {
	mock.ExpectBegin()
	mock.ExpectQuery("FROM pg_locks").WillReturnRows(pgxmock.NewRows([]string{"vxid"}).AddRow(int64(42)))
	mock.ExpectExec("SELECT set_config").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("FOO").WillReturnResult(pgxmock.NewResult("FOO", 1))
	mock.ExpectCommit()
}

func TestExecuteChainCommit(t *testing.T) {
	c := Chain{ChainID: 42, OnError: "FOO", SelfDestruct: true}
	expectChain := func(mock pgxmock.PgxPoolIface) *pgxmock.ExpectedCommit {
		mock.ExpectBegin()
		mock.ExpectQuery("FROM pg_locks").WillReturnRows(pgxmock.NewRows([]string{"vxid"}).AddRow(int64(42)))
		mock.ExpectQuery("FROM timetable\\.task").WithArgs(c.ChainID).WillReturnRows(pgxmock.NewRows([]string{"task_id"}))
		return mock.ExpectCommit()
	}
	newScheduler := func(t *testing.T) (*Scheduler, pgxmock.PgxPoolIface) {
		mock, err := pgxmock.NewPool()
		assert.NoError(t, err)
		pge := pgengine.NewDB(mock, "-c", "scheduler_unit_test")
		return New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop()), mock
	}

	t.Run("commit is not bounded by DetachedTimeout", func(t *testing.T) {
		defer func(d time.Duration) { log.DetachedTimeout = d }(log.DetachedTimeout)
		log.DetachedTimeout = 50 * time.Millisecond
		sch, mock := newScheduler(t)
		expectChain(mock).WillDelayFor(150 * time.Millisecond)
		mock.ExpectExec("DELETE FROM timetable\\.chain").WithArgs(c.ChainID).WillReturnResult(pgxmock.NewResult("DELETE", 1))
		sch.executeChain(t.Context(), c)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("commit failure runs on_error and keeps chain", func(t *testing.T) {
		sch, mock := newScheduler(t)
		expectChain(mock).WillReturnError(errors.New("serialization failure"))
		expectOnError(mock)
		sch.executeChain(t.Context(), c)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestChainWorkerRemovesRunStatusOnEarlyReturn(t *testing.T) {
	const clientName = "scheduler_unit_test"
	c := Chain{ChainID: 42, MaxInstances: 16}
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "-c", clientName)
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())

	mock.ExpectExec("INSERT INTO timetable\\.active_chain").
		WithArgs(c.ChainID, clientName, c.MaxInstances).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectBegin().WillReturnError(errors.New("query_wait_timeout"))
	mock.ExpectExec("DELETE FROM timetable\\.active_chain").
		WithArgs(c.ChainID, clientName).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	ctx, cancel := context.WithCancel(context.Background())
	chains := make(chan Chain, 1)
	chains <- c
	done := make(chan struct{})
	go func() {
		sch.chainWorker(ctx, chains)
		close(done)
	}()
	assert.Eventually(t, func() bool { return mock.ExpectationsWereMet() == nil }, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-done
}

func TestExecuteChainElement(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "-c", "scheduler_unit_test", "--password=somestrong")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())
	task := &pgengine.ChainTask{Timeout: 1}

	mock.ExpectQuery("SELECT").WithArgs(pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"value"}).AddRow("foo"))
	_ = sch.executeTask(t.Context(), mock, task)
	assert.False(t, task.StartedAt.IsZero())
}

func TestExecuteOnErrorHandler(t *testing.T) {
	c := Chain{ChainID: 42, OnError: "FOO"}
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "-c", "scheduler_unit_test", "--password=somestrong")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())

	t.Run("check error handler if everything is fine", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT").WillReturnRows(pgxmock.NewRows([]string{"txid"}).AddRow(int64(42)))
		mock.ExpectExec("SELECT set_config").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
		mock.ExpectExec("FOO").WillReturnResult(pgxmock.NewResult("FOO", 1))
		mock.ExpectCommit()
		sch.executeOnErrorHandler(context.Background(), c)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("check error handler if context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		sch.executeOnErrorHandler(ctx, c)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("check error handler if begin fails", func(*testing.T) {
		mock.ExpectBegin().WillReturnError(errors.New("connection error"))
		sch.executeOnErrorHandler(context.Background(), c)
	})

	t.Run("check error handler if error", func(*testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT").WillReturnRows(pgxmock.NewRows([]string{"txid"}).AddRow(int64(42)))
		mock.ExpectExec("SELECT set_config").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
		mock.ExpectExec("FOO").WillReturnError(errors.New("Syntax error near FOO"))
		mock.ExpectCommit()
		sch.executeOnErrorHandler(context.Background(), c)
	})

	assert.NoError(t, mock.ExpectationsWereMet())
}
