package tasks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoOp(t *testing.T) {
	assert.NoError(t, taskNoOp(context.TODO(), "foo"))
}

func TestTaskSleep(t *testing.T) {
	assert.NoError(t, taskSleep(context.TODO(), "1"))
	assert.Error(t, taskSleep(context.TODO(), "foo"))
}

func TestExecuteTask(t *testing.T) {
	assert.Error(t, ExecuteTask(context.TODO(), "foo", []string{}))
	assert.Error(t, ExecuteTask(context.TODO(), "Sleep", []string{"foo"}))
	assert.NoError(t, ExecuteTask(context.TODO(), "NoOp", []string{}))
	assert.NoError(t, ExecuteTask(context.TODO(), "NoOp", []string{"foo", "bar"}))
}

func TestTaskLog(t *testing.T) {
	assert.NoError(t, taskLog(context.TODO(), "foo"))
}
