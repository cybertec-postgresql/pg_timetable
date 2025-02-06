package pgengine_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
)

var (
	mockPool pgxmock.PgxPoolIface
	mockConn pgxmock.PgxConnIface
	ctx      = context.Background()
)

func initmockdb(t *testing.T) {
	var err error
	mockPool, err = pgxmock.NewPool()
	assert.NoError(t, err)
	mockConn, err = pgxmock.NewConn()
	assert.NoError(t, err)
}

func TestIsIntervalChainListed(t *testing.T) {
	var a, b pgengine.IntervalChain
	a.ChainID = 42
	b.ChainID = 24
	assert.True(t, a.IsListed([]pgengine.IntervalChain{a, b}))
	assert.False(t, a.IsListed([]pgengine.IntervalChain{b}))
}

func TestStartTransaction(t *testing.T) {
	initmockdb(t)

	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	mockPool.ExpectBegin().WillReturnError(errors.New("foo"))
	_, txid, err := pge.StartTransaction(ctx)
	assert.Zero(t, txid)
	assert.Error(t, err)

	mockPool.ExpectBegin()
	mockPool.ExpectQuery("SELECT").WillReturnRows(pgxmock.NewRows([]string{"txid"}).AddRow(int64(42)))
	tx, txid, err := pge.StartTransaction(ctx)
	assert.NotNil(t, tx)
	assert.EqualValues(t, 42, txid)
	assert.NoError(t, err)
}

func TestMustTransaction(t *testing.T) {
	initmockdb(t)

	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	mockPool.ExpectBegin()
	mockPool.ExpectCommit().WillReturnError(errors.New("error"))
	tx, err := mockPool.Begin(context.Background())
	assert.NoError(t, err)
	pge.CommitTransaction(ctx, tx)

	mockPool.ExpectBegin()
	mockPool.ExpectRollback().WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(context.Background())
	assert.NoError(t, err)
	pge.RollbackTransaction(ctx, tx)

	mockPool.ExpectBegin()
	mockPool.ExpectExec("SAVEPOINT").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(context.Background())
	assert.NoError(t, err)
	pge.MustSavepoint(ctx, tx, 42)

	mockPool.ExpectBegin()
	mockPool.ExpectExec("ROLLBACK TO SAVEPOINT").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(context.Background())
	assert.NoError(t, err)
	pge.MustRollbackToSavepoint(ctx, tx, 42)

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestExecuteSQLTask(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	t.Run("Check autonomous SQL task", func(t *testing.T) {
		_, err := pge.ExecuteSQLTask(ctx, nil, &pgengine.ChainTask{Autonomous: true}, []string{})
		assert.ErrorContains(t, err, "pgpool.Acquire() method is not implemented")
	})

	t.Run("Check remote SQL task", func(t *testing.T) {
		task := pgengine.ChainTask{ConnectString: pgtype.Text{String: "foo", Valid: true}}
		_, err := pge.ExecuteSQLTask(ctx, nil, &task, []string{})
		assert.ErrorContains(t, err, "cannot parse")
	})

	t.Run("Check local SQL task", func(t *testing.T) {
		mockPool.ExpectBegin()
		tx, err := mockPool.Begin(ctx)
		assert.NoError(t, err)
		_, err = pge.ExecuteSQLTask(ctx, tx, &pgengine.ChainTask{IgnoreError: true}, []string{})
		assert.ErrorContains(t, err, "SQL command cannot be empty")
	})
}

func TestExecLocalSQLTask(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	mockPool.ExpectExec("SET ROLE").WillReturnResult(pgconn.NewCommandTag("SET"))
	mockPool.ExpectExec("SAVEPOINT task").WillReturnResult(pgconn.NewCommandTag("SAVEPOINT"))
	mockPool.ExpectExec("SELECT set_config").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgconn.NewCommandTag("SELECT"))
	mockPool.ExpectExec("FOO").WillReturnError(errors.New(`ERROR:  syntax error at or near "FOO"`))
	mockPool.ExpectExec("ROLLBACK TO SAVEPOINT").WillReturnResult(pgconn.NewCommandTag("ROLLBACK"))
	mockPool.ExpectExec("RESET ROLE").WillReturnResult(pgconn.NewCommandTag("RESET"))
	task := pgengine.ChainTask{
		TaskID:      42,
		IgnoreError: true,
		Script:      "FOO",
		RunAs:       pgtype.Text{String: "Bob", Valid: true},
	}
	_, err := pge.ExecLocalSQLTask(ctx, mockPool, &task, []string{})
	assert.Error(t, err)

	mockPool.ExpectExec("SET ROLE").WillReturnError(errors.New("unknown role Bob"))
	_, err = pge.ExecLocalSQLTask(ctx, mockPool, &task, []string{})
	assert.ErrorContains(t, err, "unknown role Bob")
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

func TestExecStandaloneTask(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	mockPool.ExpectExec("SET ROLE").WillReturnResult(pgconn.NewCommandTag("SET"))
	mockPool.ExpectExec("SELECT set_config").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgconn.NewCommandTag("SELECT"))
	mockPool.ExpectExec("FOO").WillReturnError(errors.New(`ERROR:  syntax error at or near "FOO"`))
	mockPool.ExpectClose()
	task := pgengine.ChainTask{
		TaskID:      42,
		IgnoreError: true,
		Script:      "FOO",
		RunAs:       pgtype.Text{String: "Bob", Valid: true},
	}
	cf := func() (pgengine.PgxConnIface, error) { return mockPool.AsConn(), nil }

	_, err := pge.ExecStandaloneTask(ctx, cf, &task, []string{})
	assert.Error(t, err)

	mockPool.ExpectExec("SET ROLE").WillReturnError(errors.New("unknown role Bob"))
	mockPool.ExpectClose()
	_, err = pge.ExecStandaloneTask(ctx, cf, &task, []string{})
	assert.ErrorContains(t, err, "unknown role Bob")

	cf = func() (pgengine.PgxConnIface, error) { return nil, errors.New("no connection") }
	_, err = pge.ExecStandaloneTask(ctx, cf, &task, []string{})
	assert.ErrorContains(t, err, "no connection")

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

func TestExpectedCloseError(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	mockConn.ExpectClose().WillReturnError(errors.New("Close failed"))
	pge.FinalizeDBConnection(context.TODO(), mockConn)

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestExecuteSQLCommand(t *testing.T) {
	initmockdb(t)

	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	_, err := pge.ExecuteSQLCommand(ctx, mockPool, "", []string{})
	assert.Error(t, err)

	mockPool.ExpectExec("correct json").WillReturnResult(pgxmock.NewResult("EXECUTE", 0))
	_, err = pge.ExecuteSQLCommand(ctx, mockPool, "correct json", []string{})
	assert.NoError(t, err)

	mockPool.ExpectExec("correct json").WithArgs("John", 30.0, nil).WillReturnResult(pgxmock.NewResult("EXECUTE", 0))
	_, err = pge.ExecuteSQLCommand(ctx, mockPool, "correct json", []string{`["John", 30, null]`})
	assert.NoError(t, err)

	mockPool.ExpectExec("incorrect json").WillReturnError(json.Unmarshal([]byte("foo"), &struct{}{}))
	_, err = pge.ExecuteSQLCommand(ctx, mockPool, "incorrect json", []string{"foo"})
	assert.Error(t, err)
}

func TestGetChainElements(t *testing.T) {
	initmockdb(t)

	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	mockPool.ExpectQuery("SELECT").WithArgs(0).WillReturnError(errors.New("error"))
	assert.Error(t, pge.GetChainElements(ctx, &[]pgengine.ChainTask{}, 0))

	mockPool.ExpectQuery("SELECT").WithArgs(0).WillReturnRows(
		pgxmock.NewRows([]string{"task_id", "command", "kind", "run_as",
			"ignore_error", "autonomous", "database_connection", "timeout"}).
			AddRow(24, "foo", "sql", "user", false, false, "postgres://foo@boo/bar", 0))
	assert.NoError(t, pge.GetChainElements(ctx, &[]pgengine.ChainTask{}, 0))

	mockPool.ExpectQuery("SELECT").WithArgs(0).WillReturnError(errors.New("error"))
	assert.Error(t, pge.GetChainParamValues(ctx, &[]string{}, &pgengine.ChainTask{}))

	mockPool.ExpectQuery("SELECT").WithArgs(0).WillReturnRows(pgxmock.NewRows([]string{"s"}).AddRow("foo"))
	assert.NoError(t, pge.GetChainParamValues(ctx, &[]string{}, &pgengine.ChainTask{}))
}

func TestSetRole(t *testing.T) {
	initmockdb(t)

	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	mockPool.ExpectBegin()
	mockPool.ExpectExec("SET ROLE").WillReturnError(errors.New("error"))
	tx, err := mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.Error(t, pge.SetRole(ctx, tx, pgtype.Text{String: "foo", Valid: true}))
	assert.NoError(t, pge.SetRole(ctx, tx, pgtype.Text{String: "", Valid: false}), "Should ignore empty run_as")

	mockPool.ExpectBegin()
	mockPool.ExpectExec("RESET ROLE").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	pge.ResetRole(ctx, tx)
}
