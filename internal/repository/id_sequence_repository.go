package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

func NextID(ctx context.Context, db *sql.DB, scope string) (string, error) {
	if db == nil {
		return "", errors.New("db is nil")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	id, err := NextIDTx(ctx, tx, scope)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func NextIDTx(ctx context.Context, tx *sql.Tx, scope string) (string, error) {
	if tx == nil {
		return "", errors.New("tx is nil")
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "", errors.New("scope is required")
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO id_sequences(scope, next_id, updated_at)
VALUES (?, 1, ?)
ON CONFLICT(scope) DO NOTHING`, scope, now); err != nil {
		return "", err
	}

	var nextID int64
	if err := tx.QueryRowContext(ctx, `SELECT next_id FROM id_sequences WHERE scope = ?`, scope).Scan(&nextID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE id_sequences
SET next_id = ?, updated_at = ?
WHERE scope = ?`, nextID+1, now, scope); err != nil {
		return "", err
	}
	return strconv.FormatInt(nextID, 10), nil
}
