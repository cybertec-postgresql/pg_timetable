package log_test

import (
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	assert.NotNil(t, log.Init("debug"))
	l := log.Init("foobar")
	assert.Equal(t, l.Level, logrus.InfoLevel)
}
