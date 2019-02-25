package pgengine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCase(t *testing.T) func(t *testing.T) {
	ClientName = "pgengine_unit_test"
	t.Log("Setup test case")
	InitAndTestConfigDBConnection("localhost", "5432", "timetable", "scheduler",
		"scheduler", "disable", SQLSchemaFiles)
	return func(t *testing.T) {
		ConfigDb.MustExec("DROP SCHEMA IF EXISTS timetable CASCADE")
		t.Log("Test schema dropped")
	}
}

func TestBootstrapSQLFileExists(t *testing.T) {
	for _, f := range SQLSchemaFiles {
		assert.FileExists(t, f, "Bootstrap file doesn't exist")
	}
}

func TestCreateConfigDBSchemaWithoutFile(t *testing.T) {
	assert.Panics(t, func() { createConfigDBSchema("wrong path") }, "Should panic with nonexistent file")
}

func TestInitAndTestConfigDBConnection(t *testing.T) {
	teardownTestCase := setupTestCase(t)
	defer teardownTestCase(t)

	require.NotNil(t, ConfigDb, "ConfigDB should be initialized")

	t.Run("Check timetable tables", func(t *testing.T) {
		var oid int
		tableNames := []string{"database_connection", "base_task", "task_chain",
			"chain_execution_config", "chain_execution_parameters",
			"log", "execution_log", "run_status"}
		for _, tableName := range tableNames {
			err := ConfigDb.Get(&oid, fmt.Sprintf("SELECT COALESCE(to_regclass('timetable.%s'), 0) :: int", tableName))
			assert.NoError(t, err, fmt.Sprintf("Query for %s existance failed", tableName))
			assert.NotEqual(t, InvalidOid, oid, fmt.Sprintf("timetable.%s function doesn't exist", tableName))
		}
	})

	t.Run("Check timetable functions", func(t *testing.T) {
		var oid int
		funcNames := []string{"_validate_json_schema_type(text, jsonb)",
			"validate_json_schema(jsonb, jsonb, jsonb)",
			"get_running_jobs(int)",
			"trig_chain_fixer()",
			"check_task(int)"}
		for _, funcName := range funcNames {
			err := ConfigDb.Get(&oid, fmt.Sprintf("SELECT COALESCE(to_regprocedure('timetable.%s'), 0) :: int", funcName))
			assert.NoError(t, err, fmt.Sprintf("Query for %s existance failed", funcName))
			assert.NotEqual(t, InvalidOid, oid, fmt.Sprintf("timetable.%s table doesn't exist", funcName))
		}
	})

	t.Run("Check log facility", func(t *testing.T) {
		var count int
		logLevels := []string{"DEBUG", "NOTICE", "LOG", "ERROR", "PANIC"}
		for _, VerboseLogLevel = range []bool{true, false} {
			ConfigDb.MustExec("TRUNCATE timetable.log")
			for _, logLevel := range logLevels {
				if logLevel == "PANIC" {
					assert.Panics(t, func() {
						LogToDB(logLevel, logLevel)
					}, "LogToDB did not panic")
				} else {
					assert.NotPanics(t, func() {
						LogToDB(logLevel, logLevel)
					}, "LogToDB panicked")
				}

				if !VerboseLogLevel {
					switch logLevel {
					case "DEBUG", "NOTICE", "LOG":
						continue
					}
				}
				err := ConfigDb.Get(&count, "SELECT count(1) FROM timetable.log WHERE log_level = $1 AND message = $2",
					logLevel, logLevel)
				assert.NoError(t, err, fmt.Sprintf("Query for %s log entry failed", logLevel))
				assert.Equal(t, 1, count, fmt.Sprintf("%s log entry doesn't exist", logLevel))
			}
		}
	})

	t.Run("Check connection closing", func(t *testing.T) {
		FinalizeConfigDBConnection()
		assert.Nil(t, ConfigDb, "Connection isn't closed properly")
		// reinit connection to execute teardown actions
		InitAndTestConfigDBConnection("localhost", "5432", "timetable", "scheduler",
			"scheduler", "disable", SQLSchemaFiles)
	})
}

func TestSchedulerFunctions(t *testing.T) {
	teardownTestCase := setupTestCase(t)
	defer teardownTestCase(t)

	t.Run("Check FixSchedulerCrash function", func(t *testing.T) {
		assert.NotPanics(t, FixSchedulerCrash, "Fix scheduler crash failed")
	})

	t.Run("Check CanProceedChainExecution funtion", func(t *testing.T) {
		assert.Equal(t, true, CanProceedChainExecution(0, 0), "Should proceed with clean database")
	})

	t.Run("Check DeleteChainConfig funtion", func(t *testing.T) {
		tx := StartTransaction()
		assert.Equal(t, false, DeleteChainConfig(tx, 0), "Should not delete in clean database")
		MustCommitTransaction(tx)
	})

	t.Run("Check GetChainElements funtion", func(t *testing.T) {
		var chains []ChainElementExecution
		tx := StartTransaction()
		assert.True(t, GetChainElements(tx, &chains, 0), "Should no error in clean database")
		assert.Empty(t, chains, "Should be empty in clean database")
		MustCommitTransaction(tx)
	})

	t.Run("Check GetChainParamValues funtion", func(t *testing.T) {
		var paramVals []string
		tx := StartTransaction()
		assert.True(t, GetChainParamValues(tx, &paramVals, &ChainElementExecution{
			ChainID:     0,
			ChainConfig: 0}), "Should no error in clean database")
		assert.Empty(t, paramVals, "Should be empty in clean database")
		MustCommitTransaction(tx)
	})

	t.Run("Check InsertChainRunStatus funtion", func(t *testing.T) {
		var id int
		tx := StartTransaction()
		assert.NotPanics(t, func() { id = InsertChainRunStatus(tx, 0, 0) }, "Should no error in clean database")
		assert.NotZero(t, id, "Run status id should be greater then 0")
		MustCommitTransaction(tx)
	})

}

func init() {
	for i := 0; i < len(SQLSchemaFiles); i++ {
		SQLSchemaFiles[i] = "../../sql/" + SQLSchemaFiles[i]
	}
}
