package ektelog

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var levelLabel = [...]string{"DEBUG", "INFO ", "WARN ", "ERROR"}

type Logger struct {
	mu    sync.Mutex
	w     io.WriteCloser
	level Level
	Path  string
}

// New åbner eller opretter logfilen på den givne sti.
func New(path string, level Level) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	l := &Logger{w: f, level: level, Path: path}
	l.Info("session startet", "pid", os.Getpid())
	return l, nil
}

// Discard returnerer en logger der kasserer al output (bruges som nul-værdi).
func Discard() *Logger {
	return &Logger{w: nopCloser{io.Discard}, level: DEBUG}
}

func (l *Logger) write(level Level, msg string, kv []any) {
	if level < l.level {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	san := func(s string) string {
		s = ansiEscape.ReplaceAllString(s, "")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\r", "\\r")
		s = strings.ReplaceAll(s, "\t", "\\t")
		return s
	}
	var sb strings.Builder
	sb.WriteString(ts)
	sb.WriteByte(' ')
	sb.WriteString(levelLabel[level])
	sb.WriteString("  ")
	sb.WriteString(san(msg))
	for i := 0; i+1 < len(kv); i += 2 {
		k := san(fmt.Sprintf("%v", kv[i]))
		v := san(fmt.Sprintf("%v", kv[i+1]))
		sb.WriteString(fmt.Sprintf("  %s=%s", k, v))
	}
	sb.WriteByte('\n')

	l.mu.Lock()
	_, _ = io.WriteString(l.w, sb.String())
	l.mu.Unlock()
}

func (l *Logger) Debug(msg string, kv ...any) { l.write(DEBUG, msg, kv) }
func (l *Logger) Info(msg string, kv ...any)  { l.write(INFO, msg, kv) }
func (l *Logger) Warn(msg string, kv ...any)  { l.write(WARN, msg, kv) }
func (l *Logger) Error(msg string, kv ...any) { l.write(ERROR, msg, kv) }

func (l *Logger) Close() {
	if l.w != nil {
		l.Info("session slut")
		_ = l.w.Close()
	}
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }
