package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureParentDir_CreatesMissingDirectories(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "nested", "dir", "test.db")
	parentDir := filepath.Dir(dbPath)

	if err := ensureParentDir(dbPath); err != nil {
		t.Fatalf("ensure parent dir: %v", err)
	}

	info, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("stat parent dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", parentDir)
	}
}

func TestOpenSQLite_CreatesDatabaseAndAppliesPragmas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "nested", "dir", "test.db")
	sqlDB, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("stat database file: %v", err)
	}

	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)

	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE parent(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create parent table: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE child(parent_id INTEGER NOT NULL REFERENCES parent(id))`); err != nil {
		t.Fatalf("create child table: %v", err)
	}

	firstConn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("open first conn: %v", err)
	}
	defer firstConn.Close()

	secondConn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("open second conn: %v", err)
	}
	defer secondConn.Close()

	var foreignKeys int
	if err := secondConn.QueryRowContext(ctx, `PRAGMA foreign_keys;`).Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign_keys pragma to be 1, got %d", foreignKeys)
	}

	var journalMode string
	if err := secondConn.QueryRowContext(ctx, `PRAGMA journal_mode;`).Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode pragma to be wal, got %q", journalMode)
	}

	if _, err := secondConn.ExecContext(ctx, `INSERT INTO child(parent_id) VALUES (1)`); err == nil {
		t.Fatalf("expected foreign key enforcement error")
	}
}

func TestOpenSQLite_ReturnsErrorWhenParentPathIsFile(t *testing.T) {
	t.Parallel()

	parentPath := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parentPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}

	sqlDB, err := OpenSQLite(filepath.Join(parentPath, "test.db"))
	if err == nil {
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		t.Fatalf("expected open sqlite to fail")
	}
	if sqlDB != nil {
		t.Fatalf("expected nil db on error")
	}
}
