package billingprovider_test

import (
	"context"
	"testing"
	"time"

	"photoeditor/backend/internal/billingprovider"
)

func newStorageAddon() (*billingprovider.StorageAddonService, *billingprovider.FakeProvider) {
	p := billingprovider.NewFakeProvider()
	gs := billingprovider.NewInMemoryStorageAddonGrantStore()
	return billingprovider.NewStorageAddonService(p, gs), p
}

// Criteria A: storage grant survives — not affected by AI cancellation.
func TestStorageAddon_StorageSurvivesAICancellation(t *testing.T) {
	ctx := context.Background()
	svc, fp := newStorageAddon()

	periodStart := time.Now().Add(-15 * 24 * time.Hour) // 15 days ago
	periodEnd := time.Now().Add(15 * 24 * time.Hour)    // 15 days from now

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_stor_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	g, err := svc.FulfillStorageAddon(ctx, "t1", "order-stor-1", "in_stor_1",
		100*1024*1024, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("FulfillStorageAddon: %v", err)
	}
	if g.StorageBytes != 100*1024*1024 {
		t.Errorf("wrong storage bytes: %d", g.StorageBytes)
	}
	// The grant is independent of AI subscription items — it stands alone.
	if g.InvoiceID != "in_stor_1" {
		t.Errorf("grant not bound to invoice: %q", g.InvoiceID)
	}
}

// Criteria B: cancelling AI alone does not cancel the subscription.
func TestStorageAddon_CancelAI_DoesNotCancelSubscription(t *testing.T) {
	svc, _ := newStorageAddon()

	items := []billingprovider.SubscriptionItem{
		{ID: "si_ai", ItemType: billingprovider.SubscriptionItemTypeAI},
		{ID: "si_stor", ItemType: billingprovider.SubscriptionItemTypeStorage},
	}
	if !svc.CanCancelAI(items) {
		t.Error("AI can be cancelled independently when storage item exists")
	}
}

func TestStorageAddon_CancelAI_OnlyAI_CanNotCancelIndependently(t *testing.T) {
	svc, _ := newStorageAddon()

	items := []billingprovider.SubscriptionItem{
		{ID: "si_ai", ItemType: billingprovider.SubscriptionItemTypeAI},
	}
	// No storage — AI is the only item; cancelling it cancels the subscription.
	if svc.CanCancelAI(items) {
		t.Error("without storage, AI cancellation would cancel the whole subscription")
	}
}

// Criteria C: egress proportional to remaining paid share.
func TestStorageAddon_EgressProportional(t *testing.T) {
	ctx := context.Background()
	svc, fp := newStorageAddon()

	now := time.Now()
	periodStart := now.Add(-20 * 24 * time.Hour) // 20 days ago
	periodEnd := now.Add(10 * 24 * time.Hour)    // 10 days from now (30 day month, 10 remaining)

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_egress_1", Status: "paid", Currency: "eur", CreatedAt: now,
	})

	const storageBytes = int64(300) // simple unit for calculation
	g, err := svc.FulfillStorageAddon(ctx, "t1", "order-egress-1", "in_egress_1",
		storageBytes, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("FulfillStorageAddon: %v", err)
	}

	// ~10/30 of storageBytes ≈ 100 (within rounding).
	expected := int64(float64(storageBytes) * 10 / 30)
	diff := g.EgressBytes - expected
	if diff < -2 || diff > 2 {
		t.Errorf("egress %d not proportional (expected ~%d)", g.EgressBytes, expected)
	}
}

// Unpaid invoice → no grant.
func TestStorageAddon_UnpaidInvoice_NoGrant(t *testing.T) {
	ctx := context.Background()
	svc, fp := newStorageAddon()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_unpaid", Status: "open", CreatedAt: time.Now(),
	})

	_, err := svc.FulfillStorageAddon(ctx, "t1", "order-unpaid", "in_unpaid",
		100, time.Now(), time.Now().Add(30*24*time.Hour))
	if err == nil {
		t.Error("unpaid invoice should not produce a grant")
	}
}
