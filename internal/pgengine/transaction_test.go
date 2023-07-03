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
	ctx := context.Background()

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
	defer mockPool.Close()
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	ctx := context.Background()

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
	defer mockPool.Close()
	ctx := context.Background()
	pge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	mockPool.ExpectBegin()
	mockPool.ExpectExec("SET ROLE").WillReturnError(errors.New("error"))
	tx, err := mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.Error(t, pge.SetRole(ctx, tx, pgtype.Text{String: "foo", Valid: true}))
	assert.Error(t, pge.SetRole(ctx, tx, pgtype.Text{String: "", Valid: false}))

	mockPool.ExpectBegin()
	mockPool.ExpectExec("RESET ROLE").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	pge.ResetRole(ctx, tx)
}
