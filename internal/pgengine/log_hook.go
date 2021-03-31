package pgengine

import (
	"context"
	"encoding/json"
	"os"
	"time"

	pgx "github.com/jackc/pgx/v4"
	"github.com/sirupsen/logrus"
)

type LogHook struct {
	cacheLimit      int           // hold this number of entries before flush to database
	cacheTimeout    time.Duration // wait this amount of time before flush to database
	highLoadTimeout time.Duration // wait this amount of time before skip log entry
	db              PgxPoolIface
	input           chan logrus.Entry
	ctx             context.Context
	lastError       chan error
	pid             int
	client          string
}

// NewHook creates a LogHook to be added to an instance of logger
func NewHook(ctx context.Context, db PgxPoolIface, client string, cacheLimit int) *LogHook {
	l := &LogHook{
		cacheLimit:      cacheLimit,
		cacheTimeout:    5 * time.Second,
		highLoadTimeout: 200 * time.Millisecond,
		db:              db,
		input:           make(chan logrus.Entry, cacheLimit),
		lastError:       make(chan error),
		ctx:             ctx,
		pid:             os.Getpid(),
		client:          client,
	}
	go l.poll(l.input)
	return l
}

func (hook *LogHook) Fire(entry *logrus.Entry) error {
	if hook.ctx.Err() != nil {
		return hook.ctx.Err()
	}
	select {
	case hook.input <- *entry:
		// entry sent
	case <-time.After(hook.highLoadTimeout):
		// entry dropped due to a huge load, check stdout or file for detailed log
	}
	select {
	case err := <-hook.lastError:
		return err
	default:
		return nil
	}
}

// Levels returns the available logging levels
func (hook *LogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// poll checks for incoming messages and caches them internally
// until either a maximum amount is reached, or a timeout occurs.
func (hook *LogHook) poll(input <-chan logrus.Entry) {
	cache := make([]logrus.Entry, 0, hook.cacheLimit)
	tick := time.NewTicker(hook.cacheTimeout)

	for {
		select {
		case <-hook.ctx.Done(): //check context with high priority
			return
		default:
			select {
			case entry := <-input:
				cache = append(cache, entry)
				if len(cache) < hook.cacheLimit {
					break
				}
				tick.Stop()
				hook.send(cache)
				cache = cache[:0]
				tick = time.NewTicker(hook.cacheTimeout)
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
		[]string{"ts", "client_name", "pid", "log_level", "message", "message_data"},
		pgx.CopyFromSlice(len(cache),
			func(i int) ([]interface{}, error) {
				jsonData, err := json.Marshal(cache[i].Data)
				if err != nil {
					return nil, err
				}
				return []interface{}{cache[i].Time,
					hook.client,
					hook.pid,
					adaptEntryLevel(cache[i].Level),
					cache[i].Message,
					jsonData}, nil
			}),
	)
	if err != nil {
		select {
		case hook.lastError <- err:
			//error sent to the logger
		default:
			//there is unprocessed error already
		}
	}
}
