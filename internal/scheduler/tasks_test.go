package scheduler

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/pashagolub/pgxmock"
	"github.com/stretchr/testify/assert"
)

func TestExecuteTask(t *testing.T) {
	mock, err := pgxmock.NewPool() //pgxmock.MonitorPingsOption(true)
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "scheduler_unit_test")
	pge.Verbose = false
	mocksch := New(pge)

	assert.Error(t, mocksch.executeTask(context.TODO(), "foo", []string{}))

	assert.Error(t, mocksch.executeTask(context.TODO(), "Sleep", []string{"foo"}))
	assert.NoError(t, mocksch.executeTask(context.TODO(), "Sleep", []string{"1"}))

	assert.NoError(t, mocksch.executeTask(context.TODO(), "NoOp", []string{}))
	assert.NoError(t, mocksch.executeTask(context.TODO(), "NoOp", []string{"foo", "bar"}))

	assert.NoError(t, mocksch.executeTask(context.TODO(), "Log", []string{"foo"}))

	assert.Error(t, mocksch.executeTask(context.TODO(), "CopyFromFile", []string{"foo"}), "Invalid json")
	assert.Error(t, mocksch.executeTask(context.TODO(), "CopyFromFile",
		[]string{`{"sql": "COPY foo from STDIN", "filename": "foo"}`}), "Acquire() should fail")
}
