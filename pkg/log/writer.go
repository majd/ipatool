package log

import (
	"github.com/rs/zerolog"
	"io"
	"os"
)

type Writer struct {
	stdOutWriter io.Writer
	stdErrWriter io.Writer
}

func NewWriter() *Writer {
	stdOutWriter := zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stdout
	})
	stdErrWriter := zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stderr
	})

	return &Writer{
		stdOutWriter: stdOutWriter,
		stdErrWriter: stdErrWriter,
	}
}

func (l *Writer) Write(p []byte) (n int, err error) {
	return l.stdOutWriter.Write(p)
}

func (l *Writer) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	switch level {
	case zerolog.DebugLevel, zerolog.InfoLevel, zerolog.WarnLevel:
		return l.stdOutWriter.Write(p)
	case zerolog.ErrorLevel:
		return l.stdErrWriter.Write(p)
	default:
		// return less than len(p) so that zerolog treats that as io.ErrShortWrite
		return len(p), nil
	}
}
