package pgengine_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/stretchr/testify/assert"
)

func TestInitAndTestMock(t *testing.T) {
	initmockdb(t)
	pgengine.OpenDB = func(c driver.Connector) *sql.DB {
		return db
	}
	defer db.Close()

	t.Run("Check bootstrap if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mock.ExpectPing()
		mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		assert.True(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
	})

	t.Run("Check bootstrap if ping failed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mock.ExpectPing().WillReturnError(errors.New("ping error"))
		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
	})

	t.Run("Check bootstrap if executeSchemaScripts failed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mock.ExpectPing()
		mock.ExpectQuery("SELECT EXISTS").WillReturnError(errors.New("internal error"))
		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
	})

	t.Run("Check bootstrap if startup file doesn't exist", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*time.Second+2)
		defer cancel()
		mock.ExpectPing()
		mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		cmdOpts.File = "foo"
		assert.False(t, pgengine.InitAndTestConfigDBConnection(ctx, *cmdOpts))
	})

	pgengine.OpenDB = sql.OpenDB

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}
