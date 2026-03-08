package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxSizeMB  = 20
	defaultMaxBackups = 5
)

type Logger struct {
	mu         sync.Mutex
	file       *os.File
	path       string
	size       int64
	maxBytes   int64
	maxBackups int
}

func New(path string, maxSizeMB, maxBackups int) (*Logger, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("log path is empty")
	}
	if maxSizeMB <= 0 {
		maxSizeMB = defaultMaxSizeMB
	}
	if maxBackups <= 0 {
		maxBackups = defaultMaxBackups
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	stat, statErr := f.Stat()
	size := int64(0)
	if statErr == nil {
		size = stat.Size()
	}

	return &Logger{
		file:       f,
		path:       path,
		size:       size,
		maxBytes:   int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
	}, nil
}

func (l *Logger) Path() string {
	return l.path
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *Logger) Debugf(format string, args ...any) {
	l.logf("DEBUG", format, args...)
}

func (l *Logger) Infof(format string, args ...any) {
	l.logf("INFO", format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.logf("WARN", format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.logf("ERROR", format, args...)
}

func (l *Logger) logf(level string, format string, args ...any) {
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	if msg == "" {
		return
	}
	// Keep one event per line for easy tail/grep.
	msg = strings.ReplaceAll(msg, "\n", "\\n")
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339Nano), strings.ToUpper(level), msg)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	if l.size+int64(len(line)) > l.maxBytes {
		if err := l.rotateLocked(); err != nil {
			_, _ = os.Stderr.WriteString(fmt.Sprintf("[auto-work] log rotate failed: %v\n", err))
		}
	}
	n, err := l.file.WriteString(line)
	if err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("[auto-work] log write failed: %v\n", err))
		return
	}
	l.size += int64(n)
}

func (l *Logger) rotateLocked() error {
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			return err
		}
		l.file = nil
	}

	lastPath := l.path + "." + strconv.Itoa(l.maxBackups)
	_ = os.Remove(lastPath)
	for idx := l.maxBackups - 1; idx >= 1; idx-- {
		src := l.path + "." + strconv.Itoa(idx)
		dst := l.path + "." + strconv.Itoa(idx+1)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	if _, err := os.Stat(l.path); err == nil {
		if err := os.Rename(l.path, l.path+".1"); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	l.file = f
	l.size = 0
	return nil
}
