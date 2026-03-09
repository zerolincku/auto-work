package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed sql/*.sql
var migrationFS embed.FS

func Up(ctx context.Context, db *sql.DB) error {
	if err := ensureSchemaMigrationsTable(ctx, db); err != nil {
		return err
	}

	appliedVersions, err := getAppliedVersions(ctx, db)
	if err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("sql")
	if err != nil {
		return err
	}

	type migration struct {
		version int
		name    string
		sql     string
	}

	migrations := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		version, err := parseVersion(name)
		if err != nil {
			return fmt.Errorf("parse migration name %s: %w", name, err)
		}
		if _, ok := appliedVersions[version]; ok {
			continue
		}
		b, err := migrationFS.ReadFile(filepath.Join("sql", name))
		if err != nil {
			return err
		}
		migrations = append(migrations, migration{
			version: version,
			name:    name,
			sql:     string(b),
		})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	for _, m := range migrations {
		if err := applyMigration(ctx, db, m.version, m.name, m.sql); err != nil {
			return err
		}
	}

	return nil
}

func getAppliedVersions(ctx context.Context, db *sql.DB) (map[int]struct{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int]struct{})
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		out[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func ensureSchemaMigrationsTable(ctx context.Context, db *sql.DB) error {
	const stmt = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	_, err := db.ExecContext(ctx, stmt)
	return err
}

func getCurrentVersion(ctx context.Context, db *sql.DB) (int, error) {
	row := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	var v int
	if err := row.Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

func applyMigration(ctx context.Context, db *sql.DB, version int, name, migrationSQL string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, migrationSQL); err != nil {
		return fmt.Errorf("execute migration %d (%s): %w", version, name, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name) VALUES (?, ?)`, version, name); err != nil {
		return fmt.Errorf("record migration %d (%s): %w", version, name, err)
	}

	return tx.Commit()
}

func parseVersion(name string) (int, error) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(base, "_")
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid migration filename")
	}
	return strconv.Atoi(parts[0])
}
