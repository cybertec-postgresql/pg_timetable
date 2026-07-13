package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/otel"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/assert"
)

func TestExecuteTask(t *testing.T) {
	mock, err := pgxmock.NewPool() //
	a := assert.New(t)
	a.NoError(err)
	pge := pgengine.NewDB(mock, "--log-database-level=none")
	mocksch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())
	task := &pgengine.ChainTask{}

	et := func(command string, params []string) (err error) {
		task.Command = command
		err = mocksch.executeBuiltinTask(context.TODO(), task, params)
		return
	}

	// Tests the len(param) == 0 case
	a.NoError(et("NoOp", []string{}))
	a.False(task.StartedAt.IsZero())

	a.NoError(et("Sleep", []string{"2"}))
	a.GreaterOrEqual(time.Duration(task.Duration * int64(time.Microsecond)), 2 * time.Second)
	a.LessOrEqual(task.StartedAt, time.Now().Add(-2 * time.Second))

	a.Error(et("foo", []string{}))
	a.Error(et("Sleep", []string{"foo"}))

	a.NoError(et("NoOp", []string{}))
	a.NoError(et("NoOp", []string{"foo", "bar"}))

	a.NoError(et("Log", []string{"foo"}))

	a.Error(et("CopyFromFile", []string{"foo"}), "Invalid json")
	a.Error(et("CopyFromFile", []string{`{"sql": "COPY", "filename": "foo"}`}), "Acquire() should fail")

	a.Error(et("CopyToFile", []string{"foo"}), "Invalid json")
	a.Error(et("CopyToFile", []string{`{"sql": "COPY", "filename": "foo"}`}), "Acquire() should fail")

	a.Error(et("CopyToProgram", []string{"foo"}), "Invalid json")
	a.Error(et("CopyToProgram", []string{`{"sql": "COPY", "program": "foo"}`}), "Acquire() should fail")

	a.Error(et("CopyFromProgram", []string{"foo"}), "Invalid json")
	a.Error(et("CopyFromProgram", []string{`{"sql": "COPY", "program": "foo"}`}), "Acquire() should fail")

	a.Error(et("SendMail", []string{"foo"}), "Invalid json")
	a.Error(et("SendMail", []string{`{"ServerHost":"smtp.example.com","ServerPort":587,"Username":"user"}`}))

	a.Error(et("Download", []string{"foo"}), "Invalid json")
	a.EqualError(et("Download", []string{`{"workersnum": 0, "fileurls": [] }`}),
		"files to download are not specified", "Download with empty files should fail")
	a.Error(et("Download", []string{`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "" }`}),
		"Downlod incorrect url should fail")

	a.NoError(et("Shutdown", []string{}))
}
