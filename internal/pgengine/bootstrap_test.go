package pgengine_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	pgx "github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
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
	mockPool.ExpectExec(`DELETE FROM timetable\.active_session`).
		WithArgs(mockpge.ClientName).
		WillReturnResult(pgxmock.NewResult("EXECUTE", 0))
	mockPool.ExpectClose()
	mockpge.Finalize()
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

type mockpgrow struct {
	results []any
}

func (r *mockpgrow) Scan(dest ...any) error {
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

func (m mockpgconn) QueryRow(context.Context, string, ...any) pgx.Row {
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
		r := &mockpgrow{results: []any{
			0, //procoid
		}}
		m := mockpgconn{r}
		assert.NoError(t, pge.TryLockClientName(context.Background(), m))
	})

	t.Run("locking error", func(t *testing.T) {
		r := &mockpgrow{results: []any{
			1,                           //procoid
			errors.New("locking error"), //error
		}}
		m := mockpgconn{r}
		assert.Error(t, pge.TryLockClientName(context.Background(), m))
	})

	t.Run("locking successful", func(t *testing.T) {
		r := &mockpgrow{results: []any{
			1,    //procoid
			true, //locked
		}}
		m := mockpgconn{r}
		assert.NoError(t, pge.TryLockClientName(context.Background(), m))
	})
}

func TestExecuteFileScript(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	// Create temporary directory for test files
	tmpDir := t.TempDir()

	anyArgs := func(i int) []any {
		args := make([]any, i)
		for j := range i {
			args[j] = pgxmock.AnyArg()
		}
		return args
	}

	t.Run("SQL file execution", func(t *testing.T) {
		// Create temporary SQL file
		sqlFile := filepath.Join(tmpDir, "test.sql")
		err := os.WriteFile(sqlFile, []byte("SELECT 1;"), 0644)
		assert.NoError(t, err)

		// Mock the SQL execution
		mockPool.ExpectExec("SELECT 1;").WillReturnResult(pgxmock.NewResult("SELECT", 1))

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = sqlFile

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.NoError(t, err)
	})

	t.Run("SQL file execution error", func(t *testing.T) {
		// Create temporary SQL file
		sqlFile := filepath.Join(tmpDir, "test_error.sql")
		err := os.WriteFile(sqlFile, []byte("SELECT 1;"), 0644)
		assert.NoError(t, err)

		// Mock the SQL execution with error
		mockPool.ExpectExec("SELECT 1;").WillReturnError(errors.New("SQL execution failed"))

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = sqlFile

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.Error(t, err)
	})

	t.Run("YAML file validation mode - valid file", func(t *testing.T) {
		// Create temporary YAML file
		yamlFile := filepath.Join(tmpDir, "test.yaml")
		yamlContent := `chains:
  - name: test_chain
    tasks:
      - name: test_task
        command: SELECT 1`
		err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
		assert.NoError(t, err)

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = yamlFile
		cmdOpts.Start.Validate = true

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.NoError(t, err)
	})

	t.Run("YAML file validation mode - invalid file", func(t *testing.T) {
		// Create temporary YAML file with invalid content
		yamlFile := filepath.Join(tmpDir, "invalid.yaml")
		yamlContent := `chains:
  - name: test_chain
    invalid_field: value
    - malformed yaml`
		err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
		assert.NoError(t, err)

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = yamlFile
		cmdOpts.Start.Validate = true

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		// Expect error due to invalid YAML structure
		assert.Error(t, err)
	})

	t.Run("YAML file import mode", func(t *testing.T) {
		// Create temporary YAML file
		yamlFile := filepath.Join(tmpDir, "test_import.yaml")
		yamlContent := `chains:
  - name: test_chain
    tasks:
      - name: test_task
        command: SELECT 1`
		err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
		assert.NoError(t, err)

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = yamlFile
		cmdOpts.Start.Validate = false
		cmdOpts.Start.Replace = false

		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("test_chain").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery("INSERT INTO timetable\\.chain").
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		mockPool.ExpectQuery("INSERT INTO timetable\\.task").
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.NoError(t, err)
	})

	t.Run("YML file extension", func(t *testing.T) {
		// Create temporary YML file
		ymlFile := filepath.Join(tmpDir, "test.yml")
		yamlContent := `chains:
  - name: test_chain
    tasks:
      - name: test_task
        command: SELECT 1`
		err := os.WriteFile(ymlFile, []byte(yamlContent), 0644)
		assert.NoError(t, err)

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = ymlFile
		cmdOpts.Start.Validate = true

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.NoError(t, err)
	})

	t.Run("File without extension", func(t *testing.T) {
		// Create file without extension with YAML content
		noExtFile := filepath.Join(tmpDir, "test_no_ext")
		yamlContent := `chains:
  - name: test_chain
    tasks:
      - name: test_task
        command: SELECT 1`
		err := os.WriteFile(noExtFile, []byte(yamlContent), 0644)
		assert.NoError(t, err)

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = noExtFile

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.Error(t, err)
	})

	t.Run("File not found error", func(t *testing.T) {
		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = "/nonexistent/file.sql"

		err := mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.Error(t, err)
	})

	t.Run("YAML import with replace flag", func(t *testing.T) {
		// Create temporary YAML file
		yamlFile := filepath.Join(tmpDir, "test_replace.yaml")
		yamlContent := `chains:
  - name: test_chain_replace
    tasks:
      - name: test_task
        command: SELECT 1`
		err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
		assert.NoError(t, err)

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = yamlFile
		cmdOpts.Start.Validate = false
		cmdOpts.Start.Replace = true

		mockPool.ExpectExec("SELECT timetable\\.delete_job").
			WithArgs("test_chain_replace").
			WillReturnResult(pgxmock.NewResult("DELETE", 1))
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("test_chain_replace").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery("INSERT INTO timetable\\.chain").
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		mockPool.ExpectQuery("INSERT INTO timetable\\.task").
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.NoError(t, err)
	})

	t.Run("SQL file with multiple statements", func(t *testing.T) {
		// Create temporary SQL file with multiple statements
		sqlFile := filepath.Join(tmpDir, "multi.sql")
		sqlContent := `SELECT 1;
SELECT 2;
INSERT INTO test VALUES (1);`
		err := os.WriteFile(sqlFile, []byte(sqlContent), 0644)
		assert.NoError(t, err)

		// Mock the SQL execution - use regex pattern to match the content
		mockPool.ExpectExec(`SELECT 1;.*SELECT 2;.*INSERT INTO test VALUES \(1\);`).WillReturnResult(pgxmock.NewResult("SELECT", 1))

		cmdOpts := config.CmdOptions{}
		cmdOpts.Start.File = sqlFile

		err = mockpge.ExecuteFileScript(context.Background(), cmdOpts)
		assert.NoError(t, err)
	})

}
