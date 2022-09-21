package pgengine_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v2"
	"github.com/stretchr/testify/assert"
)

var (
	mockPool pgxmock.PgxPoolIface
	mockConn pgxmock.PgxConnIface
)

func initmockdb(t *testing.T) {
	var err error
	mockPool, err = pgxmock.NewPool(pgxmock.MonitorPingsOption(true))
	assert.NoError(t, err)
	mockConn, err = pgxmock.NewConn()
	assert.NoError(t, err)
}

func TestMustTransaction(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	ctx := context.Background()
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
	pge.MustSavepoint(ctx, tx, "foo")

	mockPool.ExpectBegin()
	mockPool.ExpectExec("ROLLBACK TO SAVEPOINT").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(context.Background())
	assert.NoError(t, err)
	pge.MustRollbackToSavepoint(ctx, tx, "foo")

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestExecuteSQLTask(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	elements := []pgengine.ChainTask{
		{
			Autonomous:  true,
			IgnoreError: true,
			ConnectString: pgtype.Text{
				String: "foo",
				Valid:  true},
		},
		{
			Autonomous:  false,
			IgnoreError: true,
			ConnectString: pgtype.Text{
				String: "foo",
				Valid:  true},
		},
		{
			Autonomous:  false,
			IgnoreError: true,
			ConnectString: pgtype.Text{
				String: "error",
				Valid:  true},
		},
		{
			RunAs:         pgtype.Text{String: "foo", Valid: true},
			ConnectString: pgtype.Text{Valid: false},
		},
		{Autonomous: false, IgnoreError: true, ConnectString: pgtype.Text{Valid: false}},
	}

	for _, element := range elements {
		mockPool.ExpectBegin()
		tx, err := mockPool.Begin(context.Background())
		assert.NoError(t, err)
		_, _ = pge.ExecuteSQLTask(context.Background(), tx, &element, []string{})
	}
}

func TestExpectedCloseError(t *testing.T) {
	initmockdb(t)
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	mockConn.ExpectClose().WillReturnError(errors.New("Close failed"))
	pge.FinalizeRemoteDBConnection(context.TODO(), mockConn)

	assert.NoError(t, mockPool.ExpectationsWereMet(), "there were unfulfilled expectations")
}

func TestExecuteSQLCommand(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	sqlresults := []struct {
		sql    string
		params []string
		err    error
	}{
		{
			sql:    "",
			params: []string{},
			err:    errors.New("SQL command cannot be empty"),
		},
		{
			sql:    "foo",
			params: []string{},
			err:    nil,
		},
		{
			sql:    "correct json",
			params: []string{`["John", 30, null]`},
			err:    nil,
		},
		{
			sql:    "incorrect json",
			params: []string{"foo"},
			err: func(s string) error {
				return json.Unmarshal([]byte(s), &struct{}{})
			}("foo"),
		},
	}
	ctx := context.Background()
	for _, res := range sqlresults {
		if res.sql != "" {
			mockPool.ExpectExec(res.sql).WillReturnResult(pgxmock.NewResult("EXECUTE", 0))
		}
		_, err := pge.ExecuteSQLCommand(ctx, mockPool, res.sql, res.params)
		assert.Equal(t, res.err, err)
	}
}

func TestGetChainElements(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	ctx := context.Background()

	mockPool.ExpectBegin()
	mockPool.ExpectQuery("SELECT").WillReturnError(errors.New("error"))
	tx, err := mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.Error(t, pge.GetChainElements(ctx, tx, &[]pgengine.ChainTask{}, 0))

	mockPool.ExpectBegin()
	mockPool.ExpectQuery("SELECT").WithArgs(0).WillReturnRows(
		pgxmock.NewRows([]string{"task_id", "command", "kind", "run_as",
			"ignore_error", "autonomous", "database_connection", "timeout"}).
			AddRow(24, "foo", "sql", "user", false, false, "postgres://foo@boo/bar", 0))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.NoError(t, pge.GetChainElements(ctx, tx, &[]pgengine.ChainTask{}, 0))

	mockPool.ExpectBegin()
	mockPool.ExpectQuery("SELECT").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.Error(t, pge.GetChainParamValues(ctx, tx, &[]string{}, &pgengine.ChainTask{}))

	mockPool.ExpectBegin()
	mockPool.ExpectQuery("SELECT").WithArgs(0).WillReturnRows(pgxmock.NewRows([]string{"s"}).AddRow("foo"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.NoError(t, pge.GetChainParamValues(ctx, tx, &[]string{}, &pgengine.ChainTask{}))
}

func TestSetRole(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	ctx := context.Background()
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	mockPool.ExpectBegin()
	mockPool.ExpectExec("SET ROLE").WillReturnError(errors.New("error"))
	tx, err := mockPool.Begin(ctx)
	assert.NoError(t, err)
	pge.SetRole(ctx, tx, pgtype.Text{String: "foo", Valid: true})

	mockPool.ExpectBegin()
	mockPool.ExpectExec("RESET ROLE").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	pge.ResetRole(ctx, tx)
}
