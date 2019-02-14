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
}
