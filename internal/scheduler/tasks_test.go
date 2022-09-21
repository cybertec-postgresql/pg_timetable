package scheduler

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/pashagolub/pgxmock/v2"
	"github.com/stretchr/testify/assert"
)

func TestExecuteTask(t *testing.T) {
	mock, err := pgxmock.NewPool() //pgxmock.MonitorPingsOption(true)
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "scheduler_unit_test")
	mocksch := New(pge, log.Init(config.LoggingOpts{LogLevel: "error"}))

	et := func(task string, params []string) (err error) {
		_, err = mocksch.executeTask(context.TODO(), task, params)
		return
	}

	assert.Error(t, et("foo", []string{}))

	assert.Error(t, et("Sleep", []string{"foo"}))
	assert.NoError(t, et("Sleep", []string{"1"}))

	assert.NoError(t, et("NoOp", []string{}))
	assert.NoError(t, et("NoOp", []string{"foo", "bar"}))

	assert.NoError(t, et("Log", []string{"foo"}))

	assert.Error(t, et("CopyFromFile", []string{"foo"}), "Invalid json")
	assert.Error(t, et("CopyFromFile", []string{`{"sql": "COPY", "filename": "foo"}`}), "Acquire() should fail")

	assert.Error(t, et("CopyToFile", []string{"foo"}), "Invalid json")
	assert.Error(t, et("CopyToFile", []string{`{"sql": "COPY", "filename": "foo"}`}), "Acquire() should fail")

	assert.Error(t, et("SendMail", []string{"foo"}), "Invalid json")
	assert.Error(t, et("SendMail", []string{`{"ServerHost":"smtp.example.com","ServerPort":587,"Username":"user"}`}))

	assert.Error(t, et("Download", []string{"foo"}), "Invalid json")
	assert.EqualError(t, et("Download", []string{`{"workersnum": 0, "fileurls": [] }`}),
		"Files to download are not specified", "Download with empty files should fail")
	assert.Error(t, et("Download", []string{`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "" }`}),
		"Downlod incorrect url should fail")

	assert.NoError(t, et("Shutdown", []string{}))
}
