package log

import (
	"context"

	"github.com/jackc/pgx/v4"
	"github.com/sirupsen/logrus"
)

type (
	LoggerIface logrus.FieldLogger
	loggerKey   struct{}
)

type PgxLogger struct {
}

func NewPgxLogger() *PgxLogger {
	return &PgxLogger{}
}

// Init creates logging facilities for the application
func Init(level string) LoggerIface {
	var err error
	l := logrus.New()
	l.Level, err = logrus.ParseLevel(level)
	if err != nil {
		l.Level = logrus.InfoLevel
	}
	l.SetFormatter(&Formatter{
		HideKeys:        false,
		FieldsOrder:     []string{"chain", "task", "sql", "params"},
		TimestampFormat: "2006-01-02 15:04:05.000",
		ShowFullLevel:   true,
	})
	l.SetReportCaller(l.Level > logrus.InfoLevel)
	return l
}

func (l *PgxLogger) Log(ctx context.Context, level pgx.LogLevel, msg string, data map[string]interface{}) {
	var logger logrus.FieldLogger
	if data != nil {
		logger = GetLogger(ctx).WithFields(data)
	} else {
		logger = GetLogger(ctx)
	}
	switch level {
	case pgx.LogLevelTrace:
		logger.WithField("PGX_LOG_LEVEL", level).Debug(msg)
	case pgx.LogLevelDebug, pgx.LogLevelInfo: //pgx is way too chatty on INFO level
		logger.Debug(msg)
	case pgx.LogLevelWarn:
		logger.Warn(msg)
	case pgx.LogLevelError:
		logger.Error(msg)
	default:
		logger.WithField("INVALID_PGX_LOG_LEVEL", level).Error(msg)
	}
}

// WithLogger returns a new context with the provided logger. Use in
// combination with logger.WithField(s) for great effect
func WithLogger(ctx context.Context, logger LoggerIface) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// L is an alias for the standard logger
var L = logrus.NewEntry(logrus.StandardLogger())

// GetLogger retrieves the current logger from the context. If no logger is
// available, the default logger is returned
func GetLogger(ctx context.Context) LoggerIface {
	logger := ctx.Value(loggerKey{})
	if logger == nil {
		return L
	}
	return logger.(LoggerIface)
}
