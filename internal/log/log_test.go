package log_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	assert.NotNil(t, log.Init(config.LoggingOpts{LogLevel: "debug"}))
	l := log.Init(config.LoggingOpts{LogLevel: "foobar"})
	assert.Equal(t, l.(*logrus.Logger).Level, logrus.InfoLevel)
	pgxl := log.NewPgxLogger(l)
	assert.NotNil(t, pgxl)
	ctx := log.WithLogger(context.Background(), l)
	assert.True(t, log.GetLogger(ctx) == l)
	assert.True(t, log.GetLogger(context.Background()) == log.FallbackLogger)
}

func TestFileLogger(t *testing.T) {
	l := log.Init(config.LoggingOpts{LogLevel: "debug", LogFile: "test.log", LogFileFormat: "text"})
	assert.Equal(t, l.(*logrus.Logger).Level, logrus.DebugLevel)
	l.Info("test")
	assert.FileExists(t, "test.log", "Log file should be created")
	_ = os.Remove("test.log")
}

func TestPgxLog(*testing.T) {
	pgxl := log.NewPgxLogger(log.Init(config.LoggingOpts{LogLevel: "trace"}))
	var level tracelog.LogLevel
	for level = tracelog.LogLevelNone; level <= tracelog.LogLevelTrace; level++ {
		pgxl.Log(context.Background(), level, "foo", map[string]any{"func": "TestPgxLog"})
	}
}

// TestPgxLoggerDropsQueryArgs: a context marked with WithoutQueryArgs
// drops the `args` key while retaining `sql`; an unmarked context retains
// both.
func TestPgxLoggerDropsQueryArgs(t *testing.T) {
	var buf bytes.Buffer
	base := logrus.New()
	base.SetOutput(&buf)
	base.SetLevel(logrus.DebugLevel)
	l := log.Init(config.LoggingOpts{LogLevel: "debug"})
	_ = l
	pgxl := log.NewPgxLogger(base)

	ctx := context.Background()
	pgxl.Log(log.WithLogger(ctx, base), tracelog.LogLevelDebug,
		"Query", map[string]any{"sql": "SELECT $1", "args": []any{"secret-value"}})
	assert.Contains(t, buf.String(), "SELECT $1")
	assert.Contains(t, buf.String(), "secret-value", "args must be logged when context is unmarked")

	buf.Reset()
	pgxl.Log(log.WithLogger(log.WithoutQueryArgs(ctx), base), tracelog.LogLevelDebug,
		"Query", map[string]any{"sql": "SELECT $1", "args": []any{"secret-value"}})
	assert.Contains(t, buf.String(), "SELECT $1", "sql must remain under WithoutQueryArgs")
	assert.NotContains(t, buf.String(), "secret-value", "args must be dropped under WithoutQueryArgs")
}
