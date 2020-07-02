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
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
)

func TestMain(m *testing.M) {
	testutils.TestMain(m)
}

func TestRun(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	require.NotNil(t, pgengine.ConfigDb, "ConfigDB should be initialized")

	ok := pgengine.ExecuteCustomScripts(context.Background(), "../../samples/Interval.sql")
	assert.True(t, ok, "Creating interval tasks failed")
	ok = pgengine.ExecuteCustomScripts(context.Background(), "../../samples/basic.sql")
	assert.True(t, ok, "Creating sql tasks failed")
	ok = pgengine.ExecuteCustomScripts(context.Background(), "../../samples/NoOp.sql")
	assert.True(t, ok, "Creating built-in tasks failed")
	ok = pgengine.ExecuteCustomScripts(context.Background(), "../../samples/Shell.sql")
	assert.True(t, ok, "Creating shell tasks failed")
	ok = pgengine.ExecuteCustomScripts(context.Background(), "../../samples/SelfDestruct.sql")
	assert.True(t, ok, "Creating shell tasks failed")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	assert.Equal(t, Run(ctx, false), ContextCancelled)

}

func TestAsyncChains(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	assert.NoError(t, err)
	pgengine.ConfigDb = sqlx.NewDb(db, "sqlmock")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//add correct chain
	pgengine.NotificationHandler(nil, &pgconn.Notification{Payload: "24"})
	mock.ExpectQuery("SELECT.+chain_execution_config").
		WillReturnRows(sqlmock.NewRows([]string{"chain_execution_config", "chain_id", "chain_name",
			"self_destruct", "exclusive_execution", "max_instances"}).
			AddRow(24, 24, "foo", false, false, 16))
	if pgengine.VerboseLogLevel {
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(sqlmock.NewResult(0, 1))
	}
	//add incorrect chaing
	pgengine.NotificationHandler(nil, &pgconn.Notification{Payload: "42"})
	mock.ExpectQuery("SELECT.+chain_execution_config").WillReturnError(errors.New("error"))
	mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(sqlmock.NewResult(0, 1))
	//emulate context cancellation
	pgengine.NotificationHandler(nil, &pgconn.Notification{Payload: "0"})
	retrieveAsyncChainsAndRun(ctx)
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
