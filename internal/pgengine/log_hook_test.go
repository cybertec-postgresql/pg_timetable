package pgengine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestLogHook(t *testing.T) {
	var h LogHook
	mockPool, err := pgxmock.NewPool(pgxmock.MonitorPingsOption(true))
	assert.NoError(t, err)
	func() { //fake NewHook
		h = LogHook{ctx: context.Background(),
			db:           mockPool,
			cacheLimit:   2,
			cacheTimeout: time.Second,
			input:        make(chan logrus.Entry, 2),
			level:        "debug",
		}
		go h.poll(h.input)
	}()

	entries := []logrus.Entry{ //2 entries by cacheLimit and 1 by cacheTimeout
		{Level: logrus.DebugLevel},
		{Level: logrus.InfoLevel},
		{Level: logrus.ErrorLevel},
		{Level: logrus.FatalLevel},
		{Level: 42},
	}
	for _, e := range entries {
		_ = h.Fire(&e)
		_ = adaptEntryLevel(e.Level)
	}

	levels := []string{"debug", "info", "warn", "fatal", "foobar"}
	for _, level := range levels {
		h.level = level
		assert.NotEmpty(t, h.Levels())
	}
	<-time.After(time.Second)
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := NewHook(ctx, &PgEngine{}, "debug")
	assert.Equal(t, h.Levels(), logrus.AllLevels)
	assert.NoError(t, h.Fire(&logrus.Entry{}))
}

func TestFireError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := NewHook(ctx, &PgEngine{}, "debug")
	err := errors.New("fire error")
	go func() { h.lastError <- err }()
	<-time.After(time.Second)
	assert.Equal(t, err, h.Fire(&logrus.Entry{}))
}
