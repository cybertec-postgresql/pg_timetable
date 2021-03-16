package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoOp(t *testing.T) {
	assert.NoError(t, taskNoOp(context.TODO(), nil, "foo"))
}

func TestTaskSleep(t *testing.T) {
	assert.NoError(t, taskSleep(context.TODO(), nil, "1"))
	assert.Error(t, taskSleep(context.TODO(), nil, "foo"))
}

func TestExecuteTask(t *testing.T) {
	mocksch := Scheduler{}
	assert.Error(t, mocksch.executeTask(context.TODO(), "foo", []string{}))
	assert.Error(t, mocksch.executeTask(context.TODO(), "Sleep", []string{"foo"}))
	assert.NoError(t, mocksch.executeTask(context.TODO(), "NoOp", []string{}))
	assert.NoError(t, mocksch.executeTask(context.TODO(), "NoOp", []string{"foo", "bar"}))
}

func TestTaskLog(t *testing.T) {
	assert.NoError(t, taskLog(context.TODO(), nil, "foo"))
}
