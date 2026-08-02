// Package logging defines the small logging port used by application and
// protocol layers.
package logging

import (
	"fmt"
	"io"
	"sync"
)

// Logger receives opt-in diagnostic events. Implementations must never receive
// passwords, tokens, client secrets, or request bodies.
type Logger interface {
	Debug(message string, keyValues ...any)
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}

// Nop returns a logger that discards all events.
func Nop() Logger {
	return nopLogger{}
}

type textLogger struct {
	writer io.Writer
	lock   sync.Mutex
}

// NewText returns a concurrency-safe, human-readable diagnostic logger.
func NewText(writer io.Writer) Logger {
	return &textLogger{writer: writer}
}

func (logger *textLogger) Debug(message string, keyValues ...any) {
	logger.lock.Lock()
	defer logger.lock.Unlock()
	_, _ = fmt.Fprintf(logger.writer, "debug: %s", message)
	for index := 0; index+1 < len(keyValues); index += 2 {
		_, _ = fmt.Fprintf(logger.writer, " %v=%v", keyValues[index], keyValues[index+1])
	}
	_, _ = fmt.Fprintln(logger.writer)
}
