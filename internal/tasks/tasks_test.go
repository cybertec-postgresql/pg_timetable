package tasks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestTaskLog(t *testing.T) {
	assert.NoError(t, taskLog("foo"))
}
