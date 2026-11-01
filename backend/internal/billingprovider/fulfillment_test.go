package billingprovider_test

import (
	"context"
	"testing"
	"time"

	"photoeditor/backend/internal/billingprovider"
)

func newFulfillment() (*billingprovider.FulfillmentService, *billingprovider.FakeProvider, *billingprovider.InMemoryGrantStore) {
	p := billingprovider.NewFakeProvider()
	gs := billingprovider.NewInMemoryGrantStore()
	return billingprovider.NewFulfillmentService(p, gs), p, gs
}

// Criteria A: one purchase → one grant regardless of how many times FulfillOneTime is called.
func TestFulfillment_OneTime_SingleGrant(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newFulfillment()

	// Seed a complete checkout session.
	cs, _ := fp.CreateCheckoutSession(ctx, "cs-k1", "cs-h1", billingprovider.CreateCheckoutSessionParams{
		CustomerID: "cus_1", PriceID: "price_1",
		SuccessURL: "https://app.test/ok", CancelURL: "https://app.test/cancel",
	})
	fp.SetCheckoutSessionStatus(cs.ID, "complete")

	g1, err := svc.FulfillOneTime(ctx, "t1", "order-1", cs.ID, "plan:pro:v1")
	if err != nil {
		t.Fatalf("first fulfill: %v", err)
	}
	g2, err := svc.FulfillOneTime(ctx, "t1", "order-1", cs.ID, "plan:pro:v1")
	if err != nil {
		t.Fatalf("second fulfill: %v", err)
	}
	if g1.ID != g2.ID {
		t.Errorf("second fulfill must return same grant: %q vs %q", g1.ID, g2.ID)
	}
	if g1.State != billingprovider.GrantStateApplied {
		t.Errorf("grant state should be applied, got %q", g1.State)
	}
}

// Criteria A: FulfillOneTime for incomplete session returns error, no grant.
func TestFulfillment_OneTime_IncompleteSession_Rejected(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newFulfillment()

	cs, _ := fp.CreateCheckoutSession(ctx, "cs-k2", "cs-h2", billingprovider.CreateCheckoutSessionParams{
		CustomerID: "cus_2", PriceID: "price_2",
		SuccessURL: "https://app.test/ok", CancelURL: "https://app.test/cancel",
	})
	// session remains "open" — not complete

	_, err := svc.FulfillOneTime(ctx, "t1", "order-2", cs.ID, "plan:pro:v1")
	if err == nil {
		t.Error("fulfillment of incomplete session should fail")
	}
}

// Criteria C: restart recovery — crash between payment-confirmed and grant-saved.
// On restart, re-read session from provider (still "complete") and re-grant.
func TestFulfillment_OneTime_RestartRecovery(t *testing.T) {
	ctx := context.Background()
	fp := billingprovider.NewFakeProvider()
	cs, _ := fp.CreateCheckoutSession(ctx, "cs-k3", "cs-h3", billingprovider.CreateCheckoutSessionParams{
		CustomerID: "cus_3", PriceID: "price_3",
		SuccessURL: "https://app.test/ok", CancelURL: "https://app.test/cancel",
	})
	fp.SetCheckoutSessionStatus(cs.ID, "complete")

	// Simulate crash: no grant in store, but session already complete.
	gs := billingprovider.NewInMemoryGrantStore()
	svc := billingprovider.NewFulfillmentService(fp, gs)

	g, err := svc.FulfillOneTime(ctx, "t1", "order-3", cs.ID, "plan:pro:v1")
	if err != nil {
		t.Fatalf("recovery fulfill: %v", err)
	}
	if g.State != billingprovider.GrantStateApplied {
		t.Errorf("recovered grant should be applied, got %q", g.State)
	}
}

// Criteria B: recurring grant bound to invoice/line/period.
func TestFulfillment_Recurring_BoundToInvoice(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newFulfillment()

	periodStart := time.Now().Truncate(24 * time.Hour)
	periodEnd := periodStart.AddDate(0, 1, 0)

	fp.SeedInvoice(billingprovider.Invoice{
		ID:         "in_recurring_1",
		CustomerID: "cus_1",
		Status:     "paid",
		AmountDue:  999,
		Currency:   "eur",
		CreatedAt:  time.Now(),
	})

	g, err := svc.FulfillRecurring(ctx, "t1", "order-r1", "in_recurring_1", "line_1", "plan:pro:v1", periodStart, periodEnd)
	if err != nil {
		t.Fatalf("FulfillRecurring: %v", err)
	}
	if g.InvoiceID != "in_recurring_1" {
		t.Errorf("grant not bound to invoice: %q", g.InvoiceID)
	}
	if g.PeriodStart != periodStart || g.PeriodEnd != periodEnd {
		t.Error("grant period mismatch")
	}
}

// Criteria A+B: same invoiceID on two events → one grant (no double-grant).
func TestFulfillment_Recurring_DuplicateEvent_NoDoubleGrant(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newFulfillment()

	fp.SeedInvoice(billingprovider.Invoice{
		ID:        "in_dup_1",
		Status:    "paid",
		Currency:  "eur",
		CreatedAt: time.Now(),
	})

	period := time.Now()
	g1, err := svc.FulfillRecurring(ctx, "t1", "order-dup", "in_dup_1", "li_1", "plan:pro:v1", period, period.Add(30*24*time.Hour))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	g2, err := svc.FulfillRecurring(ctx, "t1", "order-dup", "in_dup_1", "li_1", "plan:pro:v1", period, period.Add(30*24*time.Hour))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if g1.ID != g2.ID {
		t.Errorf("duplicate event produced second grant: %q vs %q", g1.ID, g2.ID)
	}
}

// Criteria B: unpaid invoice is rejected; no grant issued.
func TestFulfillment_Recurring_UnpaidInvoice_Rejected(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newFulfillment()

	fp.SeedInvoice(billingprovider.Invoice{
		ID:        "in_open_1",
		Status:    "open",
		Currency:  "eur",
		CreatedAt: time.Now(),
	})

	period := time.Now()
	_, err := svc.FulfillRecurring(ctx, "t1", "order-open", "in_open_1", "li_1", "plan:pro:v1", period, period.Add(30*24*time.Hour))
	if err == nil {
		t.Error("unpaid invoice should not produce a grant")
	}
}
