package tasks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestNoOp(t *testing.T) {
	assert.NoError(t, taskNoOp("foo"))
}

func TestTaskSleep(t *testing.T) {
	assert.NoError(t, taskSleep("1"))
	assert.Error(t, taskSleep("foo"))
}

func TestExecuteTask(t *testing.T) {
	assert.Error(t, ExecuteTask("foo", []string{}))
	assert.Error(t, ExecuteTask("Sleep", []string{"foo"}))
	assert.NoError(t, ExecuteTask("NoOp", []string{}))
	assert.NoError(t, ExecuteTask("NoOp", []string{"foo", "bar"}))
}
