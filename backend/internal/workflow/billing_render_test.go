package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestRenderBillingPassReconcilesBeforeLegacyOperations(t *testing.T) {
	rows := []expansionRow{{values: []any{true}}}
	for i := 0; i < 5; i++ {
		rows = append(rows, expansionRow{values: []any{false}})
	}
	db := &renderBatchQueries{rows: rows}
	changed, err := (RenderBatchFinalizer{DB: db, Billing: true}).ProcessOne(context.Background())
	if err != nil || !changed || len(db.calls) != 6 || db.calls[0] != "SELECT photo.billing_settle_next_render()" {
		t.Fatal(changed, err, db.calls)
	}
	sentinel := errors.New("settlement failed")
	db = &renderBatchQueries{rows: []expansionRow{{err: sentinel}}}
	if changed, err = (RenderBatchFinalizer{DB: db, Billing: true}).ProcessOne(context.Background()); changed || !errors.Is(err, sentinel) {
		t.Fatal(changed, err)
	}
}
