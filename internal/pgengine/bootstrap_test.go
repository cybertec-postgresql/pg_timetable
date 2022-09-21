package pgengine_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	pgx "github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v2"
	"github.com/stretchr/testify/assert"
)

func TestExecuteSchemaScripts(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	t.Run("Check schema scripts if error returned for SELECT EXISTS", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mockPool.ExpectQuery("SELECT EXISTS").WillReturnError(errors.New("expected"))
		assert.Error(t, mockpge.ExecuteSchemaScripts(ctx))
	})

	t.Run("Check schema scripts if error returned", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mockPool.ExpectQuery("SELECT EXISTS").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectExec("CREATE SCHEMA timetable").WillReturnError(errors.New("expected"))
		mockPool.ExpectExec("DROP SCHEMA IF EXISTS timetable CASCADE").WillReturnError(errors.New("expected"))
		assert.Error(t, mockpge.ExecuteSchemaScripts(ctx))
	})

	t.Run("Check schema scripts if everything fine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mockPool.ExpectQuery("SELECT EXISTS").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		for i := 0; i < 5; i++ {
			mockPool.ExpectExec(".*").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		}
		assert.NoError(t, mockpge.ExecuteSchemaScripts(ctx))
	})
}

func TestExecuteCustomScripts(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	t.Run("Check ExecuteCustomScripts for non-existent file", func(t *testing.T) {
		assert.Error(t, mockpge.ExecuteCustomScripts(context.Background(), "foo.bar"))
	})

	t.Run("Check ExecuteCustomScripts if error returned", func(t *testing.T) {
		mockPool.ExpectExec("SELECT timetable.add_job").WillReturnError(errors.New("expected"))
		assert.Error(t, mockpge.ExecuteCustomScripts(context.Background(), "../../samples/Basic.sql"))
	})

	t.Run("Check ExecuteCustomScripts if everything fine", func(t *testing.T) {
		mockPool.ExpectExec("SELECT timetable.add_job").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		mockPool.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		assert.NoError(t, mockpge.ExecuteCustomScripts(context.Background(), "../../samples/Basic.sql"))
	})
}

func TestFinalizeConnection(t *testing.T) {
	initmockdb(t)
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")
	mockPool.ExpectExec(`DELETE FROM timetable\.active_session`).WillReturnResult(pgxmock.NewResult("EXECUTE", 0))
	mockPool.ExpectClose()
	mockpge.Finalize()
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

type mockpgrow struct {
	results []interface{}
}

func (r *mockpgrow) Scan(dest ...interface{}) error {
	if len(r.results) > 0 {
		if err, ok := r.results[0].(error); ok {
			r.results = r.results[1:]
			return err
		}
		destv := reflect.ValueOf(dest[0])
		typ := destv.Type()
		if typ.Kind() != reflect.Ptr {
			return fmt.Errorf("dest must be pointer; got %T", destv)
		}
		destv.Elem().Set(reflect.ValueOf(r.results[0]))
		r.results = r.results[1:]
		return nil
	}
	return errors.New("mockpgrow error")
}

type mockpgconn struct {
	r pgx.Row
}

func (m mockpgconn) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return m.r
}

func TestTryLockClientName(t *testing.T) {
	pge := pgengine.NewDB(nil, "pgengine_unit_test")
	defer func() { pge = nil }()
	t.Run("query error", func(t *testing.T) {
		r := &mockpgrow{}
		m := mockpgconn{r}
		assert.Error(t, pge.TryLockClientName(context.Background(), m))
	})

	t.Run("no schema yet", func(t *testing.T) {
		r := &mockpgrow{results: []interface{}{
			0, //procoid
		}}
		m := mockpgconn{r}
		assert.NoError(t, pge.TryLockClientName(context.Background(), m))
	})

	t.Run("locking error", func(t *testing.T) {
		r := &mockpgrow{results: []interface{}{
			1,                           //procoid
			errors.New("locking error"), //error
		}}
		m := mockpgconn{r}
		assert.Error(t, pge.TryLockClientName(context.Background(), m))
	})

	t.Run("locking successful", func(t *testing.T) {
		r := &mockpgrow{results: []interface{}{
			1,    //procoid
			true, //locked
		}}
		m := mockpgconn{r}
		assert.NoError(t, pge.TryLockClientName(context.Background(), m))
	})

	t.Run("retry locking", func(t *testing.T) {
		r := &mockpgrow{results: []interface{}{
			1,     //procoid
			false, //locked
			false, //locked
			false, //locked
		}}
		m := mockpgconn{r}
		ctx, cancel := context.WithTimeout(context.Background(), pgengine.WaitTime*2)
		defer cancel()
		assert.ErrorIs(t, pge.TryLockClientName(ctx, m), ctx.Err())
	})
}
