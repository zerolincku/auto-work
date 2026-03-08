package applog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auto-work/internal/applog"
)

func TestLogger_WritesLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "auto-work.log")

	logger, err := applog.New(path, 1, 2)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	logger.Infof("hello %s", "world")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "[INFO] hello world") {
		t.Fatalf("unexpected log content: %q", text)
	}
}

func TestLogger_RotatesBySize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "auto-work.log")

	logger, err := applog.New(path, 1, 2)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	chunk := strings.Repeat("x", 35*1024)
	for i := 0; i < 80; i++ {
		logger.Infof("line=%d payload=%s", i, chunk)
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated file .1, err=%v", err)
	}
}
