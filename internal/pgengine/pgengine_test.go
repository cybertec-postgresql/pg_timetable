package pgengine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCase(t *testing.T) func(t *testing.T) {
	ClientName = "pgengine_unit_test"
	t.Log("setup test case")
	return func(t *testing.T) {
		ConfigDb.MustExec("DROP SCHEMA IF EXISTS timetable CASCADE")
		t.Log("Test schema dropped")
	}
}

func TestBootstrapSQLFileExists(t *testing.T) {
	assert.FileExists(t, "../../sql/"+SQLSchemaFile, "Bootstrap file doesn't exist")
}

func TestCreateConfigDBSchemaWithoutFile(t *testing.T) {
	assert.Panics(t, func() { createConfigDBSchema("wrong path") }, "Should panic with nonexistent file")
}

func TestInitAndTestConfigDBConnection(t *testing.T) {
	teardownTestCase := setupTestCase(t)
	defer teardownTestCase(t)

	InitAndTestConfigDBConnection("localhost", "5432", "timetable", "scheduler",
		"scheduler", "disable", "../../sql/"+SQLSchemaFile)
	require.NotNil(t, ConfigDb, "ConfigDB should be initialized")

	t.Run("Check timetable tables", func(t *testing.T) {
		var oid int
		tableNames := []string{"database_connection", "base_task", "task_chain",
			"chain_execution_config", "chain_execution_parameters",
			"log", "execution_log", "run_status"}
		for _, tableName := range tableNames {
			err := ConfigDb.Get(&oid, fmt.Sprintf("SELECT COALESCE(to_regclass('timetable.%s'), 0) :: int", tableName))
			assert.NoError(t, err, fmt.Sprintf("Query for %s existance failed", tableName))
			assert.NotEqual(t, InvalidOid, oid, fmt.Sprintf("timetable.%s table doesn't exist", tableName))
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

	t.Run("Check fix scheduler crash", func(t *testing.T) {
		assert.NotPanics(t, FixSchedulerCrash, "Fix scheduler crash failed")
	})

	t.Run("Check connection closing", func(t *testing.T) {
		FinalizeConfigDBConnection()
		assert.Nil(t, ConfigDb, "Connection isn't closed properly")
		// reinit connection to execute teardown actions
		InitAndTestConfigDBConnection("localhost", "5432", "timetable", "scheduler",
			"scheduler", "disable", "../../sql/"+SQLSchemaFile)
	})
}
