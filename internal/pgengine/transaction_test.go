package pgengine_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

var (
	db   *sql.DB
	xdb  *sqlx.DB
	mock sqlmock.Sqlmock
)

func initmockdb(t *testing.T) {
	var err error
	db, mock, err = sqlmock.New(sqlmock.MonitorPingsOption(true))
	assert.NoError(t, err)
	xdb = sqlx.NewDb(db, "sqlmock")
}

func TestMustTransaction(t *testing.T) {
	initmockdb(t)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(errors.New("error"))
	tx, err := xdb.Beginx()
	assert.NoError(t, err)
	pgengine.MustCommitTransaction(tx)

	mock.ExpectBegin()
	mock.ExpectRollback().WillReturnError(errors.New("error"))
	tx, err = xdb.Beginx()
	assert.NoError(t, err)
	pgengine.MustRollbackTransaction(tx)

	mock.ExpectBegin()
	mock.ExpectExec("SAVEPOINT").WillReturnError(errors.New("error"))
	tx, err = xdb.Beginx()
	assert.NoError(t, err)
	pgengine.MustSavepoint(tx, "foo")

	mock.ExpectBegin()
	mock.ExpectExec("ROLLBACK TO SAVEPOINT").WillReturnError(errors.New("error"))
	tx, err = xdb.Beginx()
	assert.NoError(t, err)
	pgengine.MustRollbackToSavepoint(tx, "foo")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestExecuteSQLTask(t *testing.T) {
	initmockdb(t)
	pgengine.ConfigDb = xdb

	elements := []pgengine.ChainElementExecution{
		{
			Autonomous:  true,
			IgnoreError: true,
			DatabaseConnection: sql.NullString{
				String: "foo",
				Valid:  true},
		},
		{
			Autonomous:  false,
			IgnoreError: true,
			DatabaseConnection: sql.NullString{
				String: "foo",
				Valid:  true},
		},
		{
			Autonomous:  false,
			IgnoreError: true,
			DatabaseConnection: sql.NullString{
				String: "error",
				Valid:  true},
		},
		{RunUID: sql.NullString{String: "foo", Valid: true}},
		{Autonomous: false, IgnoreError: true},
	}

	for _, element := range elements {
		mock.ExpectBegin()
		tx, err := xdb.Beginx()
		assert.NoError(t, err)
		if element.DatabaseConnection.Valid {
			q := mock.ExpectQuery("SELECT connect_string").WithArgs(element.DatabaseConnection)
			cmdOpts := cmdparser.NewCmdOptions()
			connstr := fmt.Sprintf("host='%s' port='%s' sslmode='%s' dbname='%s' user='%s' password='%s'",
				cmdOpts.Host, cmdOpts.Port, cmdOpts.SSLMode, cmdOpts.Dbname, cmdOpts.User, cmdOpts.Password)
			if element.DatabaseConnection.String == "error" {
				q.WillReturnError(errors.New("database connection error"))
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"connect_string"}).AddRow(connstr))
			}
		}
		_ = pgengine.ExecuteSQLTask(context.Background(), tx, &element, []string{})
	}
}

func TestExpectedCloseError(t *testing.T) {
	initmockdb(t)

	mock.ExpectClose().WillReturnError(errors.New("Close failed"))
	pgengine.FinalizeRemoteDBConnection(xdb)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestExecuteSQLCommand(t *testing.T) {
	initmockdb(t)
	defer db.Close()

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

	for _, res := range sqlresults {
		if res.sql != "" {
			mock.ExpectExec(res.sql).WillReturnResult(sqlmock.NewResult(0, 0))
		}
		assert.Equal(t, res.err, pgengine.ExecuteSQLCommand(xdb, res.sql, res.params))
	}
}

func TestGetChainElements(t *testing.T) {
	initmockdb(t)
	defer db.Close()

	assert.True(t, pgengine.ChainElementExecution{}.String() > "")

	mock.ExpectBegin()
	mock.ExpectQuery("WITH RECURSIVE").WillReturnError(errors.New("error"))
	tx, err := xdb.Beginx()
	assert.NoError(t, err)
	assert.False(t, pgengine.GetChainElements(tx, &[]string{}, 0))

	mock.ExpectBegin()
	mock.ExpectQuery("WITH RECURSIVE").WithArgs(0).WillReturnRows(sqlmock.NewRows([]string{"s"}).AddRow("foo"))
	tx, err = xdb.Beginx()
	assert.NoError(t, err)
	assert.True(t, pgengine.GetChainElements(tx, &[]string{}, 0))

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnError(errors.New("error"))
	tx, err = xdb.Beginx()
	assert.NoError(t, err)
	assert.False(t, pgengine.GetChainParamValues(tx, &[]string{}, &pgengine.ChainElementExecution{}))

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WithArgs(0, 0).WillReturnRows(sqlmock.NewRows([]string{"s"}).AddRow("foo"))
	tx, err = xdb.Beginx()
	assert.NoError(t, err)
	assert.True(t, pgengine.GetChainParamValues(tx, &[]string{}, &pgengine.ChainElementExecution{}))
}

func TestSetRole(t *testing.T) {
	initmockdb(t)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("SET ROLE").WillReturnError(errors.New("error"))
	tx, err := xdb.Beginx()
	assert.NoError(t, err)
	pgengine.SetRole(tx, sql.NullString{String: "foo"})

	mock.ExpectBegin()
	mock.ExpectExec("RESET ROLE").WillReturnError(errors.New("error"))
	tx, err = xdb.Beginx()
	assert.NoError(t, err)
	pgengine.ResetRole(tx)
}
