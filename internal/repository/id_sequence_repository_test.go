package repository_test

import (
	"database/sql"
	"testing"

	"auto-work/internal/repository"
)

func TestNextIDAndNextIDTxIncrementPerScope(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)

	first, err := repository.NextID(fixture.ctx, fixture.sqlDB, "custom_scope")
	if err != nil {
		t.Fatalf("next id first call: %v", err)
	}
	second, err := repository.NextID(fixture.ctx, fixture.sqlDB, "custom_scope")
	if err != nil {
		t.Fatalf("next id second call: %v", err)
	}
	if first != "1" || second != "2" {
		t.Fatalf("expected custom scope ids 1 then 2, got %q and %q", first, second)
	}

	tx, err := fixture.sqlDB.BeginTx(fixture.ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	third, err := repository.NextIDTx(fixture.ctx, tx, "custom_scope")
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("next id tx third call: %v", err)
	}
	fourth, err := repository.NextIDTx(fixture.ctx, tx, "custom_scope")
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("next id tx fourth call: %v", err)
	}
	if third != "3" || fourth != "4" {
		_ = tx.Rollback()
		t.Fatalf("expected tx ids 3 then 4, got %q and %q", third, fourth)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	fifth, err := repository.NextID(fixture.ctx, fixture.sqlDB, "custom_scope")
	if err != nil {
		t.Fatalf("next id after commit: %v", err)
	}
	if fifth != "5" {
		t.Fatalf("expected id to continue at 5 after tx commit, got %q", fifth)
	}

	otherScope, err := repository.NextID(fixture.ctx, fixture.sqlDB, "another_scope")
	if err != nil {
		t.Fatalf("next id other scope: %v", err)
	}
	if otherScope != "1" {
		t.Fatalf("expected separate scope to start at 1, got %q", otherScope)
	}
}
