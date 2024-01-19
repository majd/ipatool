package log

import (
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"
)

//go:generate go run go.uber.org/mock/mockgen -source=writer.go -destination=writer_mock.go -package log
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

func (l *writer) Write(p []byte) (int, error) {
	n, err := l.stdOutWriter.Write(p)
	if err != nil {
		return 0, fmt.Errorf("failed to write data: %w", err)
	}

	return n, nil
}

func (l *writer) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	switch level {
	case zerolog.DebugLevel, zerolog.InfoLevel, zerolog.WarnLevel:
		n, err := l.stdOutWriter.Write(p)
		if err != nil {
			return 0, fmt.Errorf("failed to write data: %w", err)
		}

		return n, nil
	case zerolog.ErrorLevel:
		n, err := l.stdErrWriter.Write(p)
		if err != nil {
			return 0, fmt.Errorf("failed to write data: %w", err)
		}

		return n, nil
	default:
		return len(p), nil
	}
}
