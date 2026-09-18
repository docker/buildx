package logutil

import (
	"bytes"
	"io"
	"sync"

	"github.com/sirupsen/logrus"
)

var (
	loggerWriters   = map[*logrus.Logger]*loggerWriter{}
	loggerWritersMu sync.Mutex
)

func Pause(l *logrus.Logger) func() {
	loggerWritersMu.Lock()
	writer, found := loggerWriters[l]
	if !found {
		writer = newLoggerWriter(l)
		loggerWriters[l] = writer
	}
	loggerWritersMu.Unlock()

	return writer.pause()
}

type loggerWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	output io.Writer
	pauses int
}

func newLoggerWriter(l *logrus.Logger) *loggerWriter {
	l.Formatter.Format(logrus.NewEntry(l))
	writer := &loggerWriter{output: l.Out}
	l.SetOutput(writer)
	return writer
}

func (w *loggerWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.pauses > 0 {
		return w.buffer.Write(p)
	}

	return w.output.Write(p)
}

func (w *loggerWriter) pause() func() {
	w.mu.Lock()
	w.pauses++
	w.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()

			w.pauses--
			if w.pauses == 0 {
				_, _ = w.buffer.WriteTo(w.output)
			}
		})
	}
}
