package log

import (
	"errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
)

const (
	DebugLevel = "debug"
	InfoLevel  = "info"
	WarnLevel  = "warn"
	ErrorLevel = "error"
)

//go:generate mockgen -source=logger.go -destination=logger_mock.go -package log
type Logger interface {
	Output(w io.Writer) zerolog.Logger
	Update(l zerolog.Logger)
	Debug() *zerolog.Event
	Info() *zerolog.Event
	Warn() *zerolog.Event
	Error() *zerolog.Event
	LevelFromString(val string) (zerolog.Level, error)
}

type logger struct {
	internalLogger zerolog.Logger
}

func NewLogger() Logger {
	return &logger{
		internalLogger: log.Logger,
	}
}

func (l *logger) Output(w io.Writer) zerolog.Logger {
	return log.Output(w)
}

func (l *logger) Update(logger zerolog.Logger) {
	l.internalLogger = logger
}

func (l *logger) Debug() *zerolog.Event {
	return l.internalLogger.Debug()
}

func (l *logger) Info() *zerolog.Event {
	return l.internalLogger.Info()
}

func (l *logger) Warn() *zerolog.Event {
	return l.internalLogger.Warn()
}

func (l *logger) Error() *zerolog.Event {
	return l.internalLogger.Error()
}

func (*logger) LevelFromString(val string) (zerolog.Level, error) {
	switch val {
	case DebugLevel:
		return zerolog.DebugLevel, nil
	case InfoLevel:
		return zerolog.InfoLevel, nil
	case WarnLevel:
		return zerolog.WarnLevel, nil
	case ErrorLevel:
		return zerolog.ErrorLevel, nil
	default:
		return 0, errors.New("invalid log level")
	}
}
