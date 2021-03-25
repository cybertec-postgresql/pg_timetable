package log_test

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/jackc/pgx/v4"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	assert.NotNil(t, log.Init("debug"))
	l := log.Init("foobar")
	assert.Equal(t, l.(*logrus.Logger).Level, logrus.InfoLevel)
	pgxl := log.NewPgxLogger(l)
	assert.NotNil(t, pgxl)
	ctx := log.WithLogger(context.Background(), l)
	assert.True(t, log.GetLogger(ctx) == l)
	assert.True(t, log.GetLogger(context.Background()) == log.FallbackLogger)
}

func TestPgxLog(t *testing.T) {
	pgxl := log.NewPgxLogger(log.Init("trace"))
	var level pgx.LogLevel
	for level = pgx.LogLevelNone; level <= pgx.LogLevelTrace; level++ {
		pgxl.Log(context.Background(), level, "foo", map[string]interface{}{"func": "TestPgxLog"})
	}
}
