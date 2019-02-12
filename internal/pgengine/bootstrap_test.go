package pgengine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func setupTestCase(t *testing.T) func(t *testing.T) {
	t.Log("setup test case")
	return func(t *testing.T) {
		ConfigDb.MustExec("DROP SCHEMA IF EXISTS timetable")
		t.Log("test schema dropped")
	}
}

func TestBootstrapSQLFileExists(t *testing.T) {
	require.FileExists(t, "../../sql/"+SQLSchemaFile, "Bootstrap file doesn't exist")
}

func TestInitAndTestConfigDBConnection(t *testing.T) {
	teardownTestCase := setupTestCase(t)
	defer teardownTestCase(t)

	InitAndTestConfigDBConnection("localhost", "5432", "timetable", "scheduler",
		"scheduler", "disable", "../../sql/"+SQLSchemaFile)

	require.NotNil(t, ConfigDb, "ConfigDB should be initialized")
}
