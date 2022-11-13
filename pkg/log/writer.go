package log

import (
	"github.com/rs/zerolog"
	"io"
	"os"
)

//go:generate mockgen -source=writer.go -destination=writer_mock.go -package log
type Writer interface {
	Write(p []byte) (n int, err error)
	WriteLevel(level zerolog.Level, p []byte) (n int, err error)
}

type writer struct {
	stdOutWriter io.Writer
	stdErrWriter io.Writer
}

func NewWriter() Writer {
	return &writer{
		stdOutWriter: zerolog.ConsoleWriter{Out: os.Stdout},
		stdErrWriter: zerolog.ConsoleWriter{Out: os.Stderr},
	}
}

func (l *writer) Write(p []byte) (n int, err error) {
	return l.stdOutWriter.Write(p)
}

func (l *writer) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	switch level {
	case zerolog.DebugLevel, zerolog.InfoLevel, zerolog.WarnLevel:
		return l.stdOutWriter.Write(p)
	case zerolog.ErrorLevel:
		return l.stdErrWriter.Write(p)
	default:
		return len(p), nil
	}
}
