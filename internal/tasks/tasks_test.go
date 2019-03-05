package tasks

import (
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

func TestDownloadFile(t *testing.T) {
	downloadUrls = func(urls []string, dest string, workers int) error { return nil }
	assert.EqualError(t, taskDownloadFile(""), `unexpected end of JSON input`,
		"Download with empty param should fail")
	assert.EqualError(t, taskDownloadFile(`{"workersnum": 0, "fileurls": [] }`),
		"Files to download are not specified", "Download with empty files should fail")
	assert.Error(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "non-existent" }`),
		"Downlod with non-existent directory or insufficient rights should fail")
	assert.NoError(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "." }`),
		"Downlod with correct json input should succeed")
}
