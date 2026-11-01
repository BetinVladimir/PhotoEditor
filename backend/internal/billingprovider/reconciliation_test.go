package billingprovider_test

import (
	"context"
	"testing"
	"time"

	"photoeditor/backend/internal/billingprovider"
)

func newReconciliation() (*billingprovider.ReconciliationService, *billingprovider.FakeProvider, *billingprovider.InMemoryGrantStore, *billingprovider.InMemoryTombstoneStore) {
	p := billingprovider.NewFakeProvider()
	gs := billingprovider.NewInMemoryGrantStore()
	ts := billingprovider.NewInMemoryTombstoneStore()
	return billingprovider.NewReconciliationService(p, gs, ts), p, gs, ts
}

// Criteria A: lost webhook → reconcile from provider invoice.
func TestReconciliation_LostWebhook_GrantRestored(t *testing.T) {
	ctx := context.Background()
	svc, fp, gs, _ := newReconciliation()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_lost_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	period := time.Now()
	rec, err := svc.ReconcileInvoice(ctx, "t1", "order-lost-1", "in_lost_1", "li_1", "plan:pro:v1",
		period, period.Add(30*24*time.Hour))
	if err != nil {
		t.Fatalf("ReconcileInvoice: %v", err)
	}
	if rec.GrantsRestored != 1 {
		t.Errorf("expected 1 restored grant, got %d", rec.GrantsRestored)
	}

	// Grant now exists in store.
	_, found, _ := gs.GetByInvoice(ctx, "in_lost_1")
	if !found {
		t.Error("grant must be in store after reconciliation")
	}
}

// Criteria A: existing grant not overwritten by reconciliation.
func TestReconciliation_ExistingGrant_NotDuplicated(t *testing.T) {
	ctx := context.Background()
	svc, fp, _, _ := newReconciliation()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_existing_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	period := time.Now()
	rec1, _ := svc.ReconcileInvoice(ctx, "t1", "order-ex-1", "in_existing_1", "li_1", "plan:pro:v1",
		period, period.Add(30*24*time.Hour))
	if rec1.GrantsRestored != 1 {
		t.Fatalf("first reconcile should restore 1 grant")
	}

	rec2, err := svc.ReconcileInvoice(ctx, "t1", "order-ex-1", "in_existing_1", "li_1", "plan:pro:v1",
		period, period.Add(30*24*time.Hour))
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if rec2.GrantsRestored != 0 {
		t.Error("second reconcile should not create duplicate grant")
	}
}

// Criteria B+C: tombstone prevents grant restoration.
func TestReconciliation_Tombstone_PreventsResurrection(t *testing.T) {
	ctx := context.Background()
	svc, fp, gs, ts := newReconciliation()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_tomb_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	ts.AddTombstone("tenant-deleted")

	period := time.Now()
	rec, err := svc.ReconcileInvoice(ctx, "tenant-deleted", "order-tomb-1", "in_tomb_1", "li_1", "plan:pro:v1",
		period, period.Add(30*24*time.Hour))
	if err != nil {
		t.Fatalf("ReconcileInvoice: %v", err)
	}
	if rec.GrantsRestored != 0 {
		t.Error("deleted tenant must not receive a restored grant (criteria C)")
	}
	if rec.TombstonesPreserved != 1 {
		t.Errorf("expected 1 tombstone preserved, got %d", rec.TombstonesPreserved)
	}

	// Grant must not be in store.
	_, found, _ := gs.GetByInvoice(ctx, "in_tomb_1")
	if found {
		t.Error("deleted account must not be resurrected — grant must not exist")
	}
}

// Unpaid invoice skipped.
func TestReconciliation_UnpaidInvoice_Skipped(t *testing.T) {
	ctx := context.Background()
	svc, fp, _, _ := newReconciliation()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_unpaid_rec", Status: "open", Currency: "eur", CreatedAt: time.Now(),
	})

	period := time.Now()
	_, err := svc.ReconcileInvoice(ctx, "t1", "order-unpaid-rec", "in_unpaid_rec", "li_1", "plan:pro:v1",
		period, period.Add(30*24*time.Hour))
	if err == nil {
		t.Error("unpaid invoice must not produce a restored grant")
	}
}
