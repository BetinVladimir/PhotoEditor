package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestOperationBillingReconciliationPreservesProgress(t *testing.T) {
	rows := []expansionRow{{values: []any{true}}, {values: []any{false}}}
	for i := 0; i < 5; i++ {
		rows = append(rows, expansionRow{values: []any{false}})
	}
	db := &renderBatchQueries{rows: rows}
	changed, err := (RenderBatchFinalizer{DB: db, OperationBilling: true, Billing: true}).ProcessOne(context.Background())
	if err != nil || !changed || len(db.calls) != 7 || db.calls[0] != "SELECT photo.billing_settle_next_operations()" || db.calls[1] != "SELECT photo.billing_settle_next_render()" {
		t.Fatal(changed, err, db.calls)
	}
	failure := errors.New("render settlement failed")
	db = &renderBatchQueries{rows: []expansionRow{{values: []any{true}}, {err: failure}}}
	if changed, err = (RenderBatchFinalizer{DB: db, OperationBilling: true, Billing: true}).ProcessOne(context.Background()); !changed || !errors.Is(err, failure) {
		t.Fatal("lost earlier operation progress", changed, err)
	}
	db = &renderBatchQueries{rows: []expansionRow{{err: failure}}}
	if changed, err = (RenderBatchFinalizer{DB: db, OperationBilling: true}).ProcessOne(context.Background()); changed || !errors.Is(err, failure) {
		t.Fatal(changed, err)
	}
}
