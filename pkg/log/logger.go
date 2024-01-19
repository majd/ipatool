package log

import (
	"io"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

//go:generate go run go.uber.org/mock/mockgen -source=logger.go -destination=logger_mock.go -package log
type Logger interface {
	Verbose() *zerolog.Event
	Log() *zerolog.Event
	Error() *zerolog.Event
}

type logger struct {
	internalLogger zerolog.Logger
	verbose        bool
}

type Args struct {
	Verbose bool
	Writer  io.Writer
}

func NewLogger(args Args) Logger {
	internalLogger := log.Logger
	level := zerolog.InfoLevel

	if args.Verbose {
		level = zerolog.DebugLevel
		zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	}

	internalLogger = internalLogger.Output(args.Writer).Level(level)

	return &logger{
		verbose:        args.Verbose,
		internalLogger: internalLogger,
	}
}

func (l *logger) Log() *zerolog.Event {
	return l.internalLogger.Info()
}

func (l *logger) Verbose() *zerolog.Event {
	if !l.verbose {
		return nil
	}

	return l.internalLogger.Debug()
}

func (l *logger) Error() *zerolog.Event {
	return l.internalLogger.Error()
}
