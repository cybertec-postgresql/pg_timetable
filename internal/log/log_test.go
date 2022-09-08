package log_test

import (
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

func TestPgxLog(t *testing.T) {
	pgxl := log.NewPgxLogger(log.Init(config.LoggingOpts{LogLevel: "trace"}))
	var level tracelog.LogLevel
	for level = tracelog.LogLevelNone; level <= tracelog.LogLevelTrace; level++ {
		pgxl.Log(context.Background(), level, "foo", map[string]interface{}{"func": "TestPgxLog"})
	}
}
