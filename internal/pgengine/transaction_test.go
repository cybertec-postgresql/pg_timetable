package pgengine_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgtype"
	"github.com/pashagolub/pgxmock"
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
	pge.MustCommitTransaction(ctx, tx)

	mockPool.ExpectBegin()
	mockPool.ExpectRollback().WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(context.Background())
	assert.NoError(t, err)
	pge.MustRollbackTransaction(ctx, tx)

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

	elements := []pgengine.ChainElement{
		{
			Autonomous:  true,
			IgnoreError: true,
			ConnectString: pgtype.Varchar{
				String: "foo",
				Status: pgtype.Present},
		},
		{
			Autonomous:  false,
			IgnoreError: true,
			ConnectString: pgtype.Varchar{
				String: "foo",
				Status: pgtype.Present},
		},
		{
			Autonomous:  false,
			IgnoreError: true,
			ConnectString: pgtype.Varchar{
				String: "error",
				Status: pgtype.Present},
		},
		{RunAs: pgtype.Varchar{String: "foo", Status: pgtype.Present}},
		{Autonomous: false, IgnoreError: true},
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
			err:    errors.New("SQL script cannot be empty"),
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
	mockPool.ExpectQuery("WITH RECURSIVE").WillReturnError(errors.New("error"))
	tx, err := mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.False(t, pge.GetChainElements(ctx, tx, &[]string{}, 0))

	mockPool.ExpectBegin()
	mockPool.ExpectQuery("WITH RECURSIVE").WithArgs(0).WillReturnRows(pgxmock.NewRows([]string{"s"}).AddRow("foo"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.True(t, pge.GetChainElements(ctx, tx, &[]string{}, 0))

	mockPool.ExpectBegin()
	mockPool.ExpectQuery("SELECT").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.False(t, pge.GetChainParamValues(ctx, tx, &[]string{}, &pgengine.ChainElement{}))

	mockPool.ExpectBegin()
	mockPool.ExpectQuery("SELECT").WithArgs(0, 0).WillReturnRows(pgxmock.NewRows([]string{"s"}).AddRow("foo"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	assert.True(t, pge.GetChainParamValues(ctx, tx, &[]string{}, &pgengine.ChainElement{}))
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
	pge.SetRole(ctx, tx, pgtype.Varchar{String: "foo"})

	mockPool.ExpectBegin()
	mockPool.ExpectExec("RESET ROLE").WillReturnError(errors.New("error"))
	tx, err = mockPool.Begin(ctx)
	assert.NoError(t, err)
	pge.ResetRole(ctx, tx)
}
