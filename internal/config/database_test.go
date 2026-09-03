package config

import (
	"context"
	"testing"
	"time"
)

type testRow struct {
	ctx context.Context
}

func (row testRow) Scan(_ ...any) error {
	if err := row.ctx.Err(); err != nil {
		return err
	}
	return nil
}

func TestDatabaseOperationContextIsBoundedAndCancelable(t *testing.T) {
	ctx, cancel := databaseOperationContext(context.Background())
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("database operation context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > DatabaseOperationTimeout {
		t.Fatalf("database operation deadline remaining = %s", remaining)
	}
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("database operation context was not canceled")
	}
}

func TestCancelRowReleasesOperationContextAfterScan(t *testing.T) {
	ctx, cancel := databaseOperationContext(context.Background())
	row := &cancelRow{Row: testRow{ctx: ctx}, cancel: cancel}
	if err := row.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("row scan did not release its operation context")
	}
}

func TestLegacyDatabaseContextDoesNotAllocateAnUnownedTimer(t *testing.T) {
	if _, ok := DatabaseContext().Deadline(); ok {
		t.Fatal("legacy database parent context unexpectedly owns a deadline")
	}
}
