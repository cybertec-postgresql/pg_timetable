package tasks

import (
	"fmt"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/stretchr/testify/assert"
)

func setupTestCase(t *testing.T) func(t *testing.T) {
	pgengine.ClientName = "scheduler_unit_test"
	pgengine.InitAndTestConfigDBConnection("localhost", "5432", "timetable", "scheduler",
		"scheduler", "disable", pgengine.SQLSchemaFiles)
	return func(t *testing.T) {
		pgengine.ConfigDb.MustExec("DROP SCHEMA IF EXISTS timetable CASCADE")
		t.Log("Test schema dropped")
		pgengine.ConfigDb.Close()
	}
}

func TestBuiltInTasks(t *testing.T) {
	teardownTestCase := setupTestCase(t)
	defer teardownTestCase(t)
	t.Run("Check built-in tasks number", func(t *testing.T) {
		var num int
		err := pgengine.ConfigDb.Get(&num, "SELECT count(1) FROM timetable.base_task WHERE kind = 'BUILTIN'")
		assert.NoError(t, err, "Query for built-in tasks existance failed")
		assert.Equal(t, len(tasks), num, fmt.Sprintf("Wrong number of built-in tasks: %d", num))
	})
}

func init() {
	for i := 0; i < len(pgengine.SQLSchemaFiles); i++ {
		pgengine.SQLSchemaFiles[i] = "../../sql/" + pgengine.SQLSchemaFiles[i]
	}
}
