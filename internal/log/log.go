package log

import (
	"context"

	"github.com/sirupsen/logrus"
)

type (
	Logger    logrus.FieldLogger
	loggerKey struct{}
)

// Init creates logging facilities for the application
func Init(level string) Logger {
	var err error
	l := logrus.New()
	l.Level, err = logrus.ParseLevel(level)
	if err != nil {
		l.Level = logrus.InfoLevel
	}
	l.SetFormatter(&Formatter{
		HideKeys:        false,
		FieldsOrder:     []string{"module", "chain"},
		TimestampFormat: "2006-01-02 15:04:05.000",
		ShowFullLevel:   true,
	})
	l.SetReportCaller(l.Level > logrus.InfoLevel)
	return l
}

// WithLogger returns a new context with the provided logger. Use in
// combination with logger.WithField(s) for great effect
func WithLogger(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// L is an alias for the standard logger
var L = logrus.NewEntry(logrus.StandardLogger())

// GetLogger retrieves the current logger from the context. If no logger is
// available, the default logger is returned
func GetLogger(ctx context.Context) Logger {
	logger := ctx.Value(loggerKey{})
	if logger == nil {
		return L
	}
	return logger.(Logger)
}
