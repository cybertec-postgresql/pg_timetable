package log

import (
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"gopkg.in/natefinch/lumberjack.v2"
)

func TestGetLogFileWriter(t *testing.T) {
	assert.IsType(t, getLogFileWriter(config.LoggingOpts{LogFileRotate: true}), &lumberjack.Logger{})
	assert.IsType(t, getLogFileWriter(config.LoggingOpts{LogFileRotate: false}), "string")
}

func TestGetLogFileFormatter(t *testing.T) {
	assert.IsType(t, getLogFileFormatter(config.LoggingOpts{LogFileFormat: "json"}), &logrus.JSONFormatter{})
	assert.IsType(t, getLogFileFormatter(config.LoggingOpts{LogFileFormat: "blah"}), &logrus.JSONFormatter{})
	assert.IsType(t, getLogFileFormatter(config.LoggingOpts{LogFileFormat: "text"}), &logrus.TextFormatter{})
}
