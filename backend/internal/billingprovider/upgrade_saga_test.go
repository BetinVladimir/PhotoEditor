package billingprovider_test

import (
	"context"
	"testing"
	"time"

	"photoeditor/backend/internal/billingprovider"
)

func newUpgradeSaga() (*billingprovider.UpgradeSagaService, *billingprovider.FakeProvider, *billingprovider.InMemoryUpgradeSagaStore) {
	p := billingprovider.NewFakeProvider()
	ss := billingprovider.NewInMemoryUpgradeSagaStore()
	return billingprovider.NewUpgradeSagaService(p, ss), p, ss
}

// Criteria A: all states and transitions are covered.
func TestUpgradeSaga_HappyPath_AllStates(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newUpgradeSaga()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_upg_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	saga, err := svc.CreateUpgrade(ctx, "tenant-1", "order-upg-1", "plan:free:v1", "plan:pro:v1", "price_pro_1")
	assertState(t, saga, err, billingprovider.UpgradeSagaStateQuoted)

	saga, err = svc.AdvanceToPaymentPending(ctx, "order-upg-1")
	assertState(t, saga, err, billingprovider.UpgradeSagaStatePaymentPending)

	saga, err = svc.MarkPaid(ctx, "order-upg-1", "in_upg_1")
	assertState(t, saga, err, billingprovider.UpgradeSagaStatePaid)

	saga, err = svc.AdvanceToPriceApplying(ctx, "order-upg-1")
	assertState(t, saga, err, billingprovider.UpgradeSagaStatePriceApplying)

	saga, err = svc.AdvanceToEntitlementApplying(ctx, "order-upg-1")
	assertState(t, saga, err, billingprovider.UpgradeSagaStateEntitlementApplying)

	saga, err = svc.MarkApplied(ctx, "order-upg-1")
	assertState(t, saga, err, billingprovider.UpgradeSagaStateApplied)

	if !saga.IsTerminal() {
		t.Error("applied saga should be terminal")
	}
}

func assertState(t *testing.T, saga billingprovider.UpgradeSaga, err error, want billingprovider.UpgradeSagaState) {
	t.Helper()
	if err != nil {
		t.Fatalf("transition to %q: %v", want, err)
	}
	if saga.State != want {
		t.Errorf("expected state %q, got %q", want, saga.State)
	}
}

// Criteria A: compensation path.
func TestUpgradeSaga_CompensationPath(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newUpgradeSaga()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_comp_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	svc.CreateUpgrade(ctx, "tenant-2", "order-comp-1", "plan:free:v1", "plan:pro:v1", "price_1")
	svc.AdvanceToPaymentPending(ctx, "order-comp-1")
	svc.MarkPaid(ctx, "order-comp-1", "in_comp_1")

	// Provider update fails — start compensation.
	saga, err := svc.StartCompensation(ctx, "order-comp-1", "provider_update_failed")
	assertState(t, saga, err, billingprovider.UpgradeSagaStateCompensating)

	saga, err = svc.MarkCompensated(ctx, "order-comp-1")
	assertState(t, saga, err, billingprovider.UpgradeSagaStateCompensated)

	if !saga.IsTerminal() {
		t.Error("compensated saga should be terminal")
	}
}

// Criteria A: manual_review path.
func TestUpgradeSaga_ManualReviewPath(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newUpgradeSaga()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_mr_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	svc.CreateUpgrade(ctx, "tenant-3", "order-mr-1", "plan:free:v1", "plan:pro:v1", "price_1")
	svc.AdvanceToPaymentPending(ctx, "order-mr-1")
	svc.MarkPaid(ctx, "order-mr-1", "in_mr_1")
	svc.StartCompensation(ctx, "order-mr-1", "refund_failed")

	saga, err := svc.MarkManualReview(ctx, "order-mr-1")
	assertState(t, saga, err, billingprovider.UpgradeSagaStateManualReview)

	if !saga.IsTerminal() {
		t.Error("manual_review saga should be terminal")
	}
}

// Criteria B: paid invoice without provider update remains paid/applying.
// UI shows "processing" and does not request payment again.
func TestUpgradeSaga_PaidWithoutProviderUpdate_StaysApplying(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newUpgradeSaga()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_stall_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	svc.CreateUpgrade(ctx, "tenant-4", "order-stall-1", "plan:free:v1", "plan:pro:v1", "price_1")
	svc.AdvanceToPaymentPending(ctx, "order-stall-1")
	saga, _ := svc.MarkPaid(ctx, "order-stall-1", "in_stall_1")

	// Provider update has not happened yet; saga is "paid" (not applied).
	if saga.State != billingprovider.UpgradeSagaStatePaid {
		t.Errorf("expected paid, got %q", saga.State)
	}

	// Calling MarkPaid again does not change state (idempotent).
	saga2, err := svc.MarkPaid(ctx, "order-stall-1", "in_stall_1")
	if err != nil {
		t.Fatalf("second MarkPaid: %v", err)
	}
	if saga2.State != billingprovider.UpgradeSagaStatePaid {
		t.Errorf("paid saga must stay paid on repeat, got %q", saga2.State)
	}
}

// Criteria C: compensation path from entitlement_applying state.
func TestUpgradeSaga_CompensationFromEntitlementApplying(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newUpgradeSaga()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_comp2_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	svc.CreateUpgrade(ctx, "tenant-5", "order-comp2-1", "plan:free:v1", "plan:pro:v1", "price_1")
	svc.AdvanceToPaymentPending(ctx, "order-comp2-1")
	svc.MarkPaid(ctx, "order-comp2-1", "in_comp2_1")
	svc.AdvanceToPriceApplying(ctx, "order-comp2-1")
	svc.AdvanceToEntitlementApplying(ctx, "order-comp2-1")

	saga, err := svc.StartCompensation(ctx, "order-comp2-1", "entitlement_grant_failed")
	assertState(t, saga, err, billingprovider.UpgradeSagaStateCompensating)
}

// Criteria D: a second upgrade before the first is closed → error.
func TestUpgradeSaga_BlocksNewUpgradeUntilClosed(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newUpgradeSaga()

	svc.CreateUpgrade(ctx, "tenant-6", "order-open-1", "plan:free:v1", "plan:pro:v1", "price_1")

	_, err := svc.CreateUpgrade(ctx, "tenant-6", "order-open-2", "plan:pro:v1", "plan:pro:v1", "price_2")
	if err == nil {
		t.Error("opening a second upgrade before closing the first must return an error")
	}
}

// After a saga reaches terminal state, a new upgrade is allowed.
func TestUpgradeSaga_NewUpgradeAfterTerminal(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newUpgradeSaga()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_after_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	svc.CreateUpgrade(ctx, "tenant-7", "order-t7-1", "plan:free:v1", "plan:pro:v1", "price_1")
	svc.AdvanceToPaymentPending(ctx, "order-t7-1")
	svc.MarkPaid(ctx, "order-t7-1", "in_after_1")
	svc.AdvanceToPriceApplying(ctx, "order-t7-1")
	svc.AdvanceToEntitlementApplying(ctx, "order-t7-1")
	svc.MarkApplied(ctx, "order-t7-1")

	// Now a new upgrade should be allowed.
	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_after_2", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})
	_, err := svc.CreateUpgrade(ctx, "tenant-7", "order-t7-2", "plan:pro:v1", "plan:studio:v1", "price_2")
	if err != nil {
		t.Errorf("new upgrade after terminal should be allowed: %v", err)
	}
}
