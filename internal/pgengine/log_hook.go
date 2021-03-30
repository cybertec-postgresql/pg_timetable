package pgengine

import (
	"context"
	"encoding/json"
	"os"
	"time"

	pgx "github.com/jackc/pgx/v4"
	"github.com/sirupsen/logrus"
)

const (
	CacheLimit   = 100
	CacheTimeout = 5 * time.Second
)

type LogHook struct {
	Extra     map[string]interface{}
	db        PgxPoolIface
	input     chan logrus.Entry
	ctx       context.Context
	lastError error
}

// NewHook creates a LogHook to be added to an instance of logger
func NewHook(ctx context.Context, db PgxPoolIface) *LogHook {
	l := &LogHook{
		db:    db,
		input: make(chan logrus.Entry, CacheLimit*2),
		ctx:   ctx,
	}
	go l.poll(l.input)
	return l
}

func (hook *LogHook) Fire(entry *logrus.Entry) (err error) {
	hook.input <- *entry
	err = hook.lastError
	hook.lastError = nil
	return
}

// Levels returns the available logging levels
func (hook *LogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// poll checks for incoming messages and caches them internally
// until either a maximum amount is reached, or a timeout occurs.
func (hook *LogHook) poll(input <-chan logrus.Entry) {
	cache := make([]logrus.Entry, 0, CacheLimit)
	tick := time.NewTicker(CacheTimeout)

	for {
		select {
		case <-hook.ctx.Done(): //check context with high priority
			return
		default:
			select {
			case entry := <-input:
				cache = append(cache, entry)
				if len(cache) < CacheLimit {
					break
				}
				tick.Stop()
				hook.send(cache)
				cache = cache[:0]
				tick = time.NewTicker(CacheTimeout)
			case <-tick.C:
				hook.send(cache)
				cache = cache[:0]
			case <-hook.ctx.Done():
				return
			}
		}
	}
}

func adaptEntryLevel(level logrus.Level) string {
	switch level {
	case logrus.TraceLevel, logrus.DebugLevel:
		return "DEBUG"
	case logrus.InfoLevel, logrus.WarnLevel:
		return "LOG"
	case logrus.ErrorLevel:
		return "ERROR"
	case logrus.FatalLevel, logrus.PanicLevel:
		return "PANIC"
	}
	return "UNKNOWN"
}

// send sends cached messages to the postgres server
func (hook *LogHook) send(cache []logrus.Entry) {
	if len(cache) == 0 {
		return // Nothing to do here.
	}
	_, err := hook.db.CopyFrom(
		hook.ctx,
		pgx.Identifier{"timetable", "log"},
		[]string{"ts", "pid", "log_level", "message", "message_data"},
		pgx.CopyFromSlice(len(cache),
			func(i int) ([]interface{}, error) {
				jsonData, err := json.Marshal(cache[i].Data)
				if err != nil {
					return nil, err
				}
				return []interface{}{cache[i].Time,
					os.Getpid(),
					adaptEntryLevel(cache[i].Level),
					cache[i].Message,
					jsonData}, nil
			}),
	)
	if err != nil {
		hook.lastError = err
	}

}
