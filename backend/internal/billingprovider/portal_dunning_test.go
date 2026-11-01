package billingprovider_test

import (
	"context"
	"testing"
	"time"

	"photoeditor/backend/internal/billingprovider"
)

// ─── PortalSessionService ───────────────────────────────────────────────────

// Criteria A: portal session is created with plan_change_mode=disabled.
func TestPortalSession_PlanChangeDisabled(t *testing.T) {
	p := billingprovider.NewFakeProvider()
	svc := billingprovider.NewPortalSessionService(p, "pconf_test_no_update")

	cus, err := p.CreateCustomer(context.Background(), "k1", "h1", billingprovider.CreateCustomerParams{Email: "test@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	res, err := svc.CreateSession(context.Background(), "t1", cus.ID, "https://return.test/", "idem-portal-1")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if res.PlanChangeMode != "disabled" {
		t.Errorf("PlanChangeMode must be %q, got %q (criteria A)", "disabled", res.PlanChangeMode)
	}
	if res.Session.URL == "" {
		t.Error("portal session URL must not be empty")
	}
}

// Criteria A: no plan change possible — PlanChangeMode is always "disabled".
func TestPortalSession_NoPlanChangeEverReturned(t *testing.T) {
	p := billingprovider.NewFakeProvider()
	svc := billingprovider.NewPortalSessionService(p, "pconf_no_update")

	cus, err := p.CreateCustomer(context.Background(), "k2", "h2", billingprovider.CreateCustomerParams{Email: "test2@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	for i := 0; i < 5; i++ {
		res, err := svc.CreateSession(context.Background(), "t2", cus.ID, "https://return.test/", "idem-p2")
		if err != nil {
			t.Fatalf("CreateSession attempt %d: %v", i, err)
		}
		if res.PlanChangeMode != "disabled" {
			t.Errorf("attempt %d: PlanChangeMode must always be disabled, got %q", i, res.PlanChangeMode)
		}
	}
}

// ─── DunningPolicy ─────────────────────────────────────────────────────────

func newDunning() (*billingprovider.DunningPolicy, *billingprovider.FakeProvider, *billingprovider.InMemoryDunningStore) {
	p := billingprovider.NewFakeProvider()
	store := billingprovider.NewInMemoryDunningStore()
	return billingprovider.NewDunningPolicy(p, store), p, store
}

// Criteria B: failed invoice → grant blocked.
func TestDunning_FailedRenewal_GrantBlocked(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newDunning()

	r, err := svc.RecordFailure(ctx, "t1", "sub_1", "in_fail_1")
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if !r.GrantBlocked {
		t.Error("grant must be blocked after renewal failure (criteria B)")
	}
	if r.State != billingprovider.DunningStateOpen {
		t.Errorf("expected state open, got %q", r.State)
	}

	blocked, err := svc.IsGrantBlocked(ctx, "in_fail_1")
	if err != nil || !blocked {
		t.Errorf("IsGrantBlocked must return true: err=%v blocked=%v", err, blocked)
	}
}

// Criteria B: paid invoice → grant unblocked after recovery.
func TestDunning_PaidInvoice_GrantUnblocked(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newDunning()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_recover_1", Status: "paid", Currency: "eur", CreatedAt: time.Now(),
	})

	svc.RecordFailure(ctx, "t1", "sub_1", "in_recover_1")
	svc.AttemptRecovery(ctx, "in_recover_1")

	r, err := svc.RecoverInvoice(ctx, "in_recover_1")
	if err != nil {
		t.Fatalf("RecoverInvoice: %v", err)
	}
	if r.GrantBlocked {
		t.Error("grant must not be blocked after recovery (criteria B)")
	}
	if r.State != billingprovider.DunningStateRecovered {
		t.Errorf("expected state recovered, got %q", r.State)
	}
}

// Criteria B: unpaid invoice must NOT be recovered.
func TestDunning_UnpaidInvoice_CannotRecover(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newDunning()

	fp.SeedInvoice(billingprovider.Invoice{
		ID: "in_unpaid_d1", Status: "open", Currency: "eur", CreatedAt: time.Now(),
	})

	svc.RecordFailure(ctx, "t1", "sub_1", "in_unpaid_d1")
	_, err := svc.RecoverInvoice(ctx, "in_unpaid_d1")
	if err == nil {
		t.Error("recovery of unpaid invoice must fail (criteria B: no grant on unpaid invoice)")
	}
}

// Idempotent RecordFailure.
func TestDunning_RecordFailure_Idempotent(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newDunning()

	r1, _ := svc.RecordFailure(ctx, "t1", "sub_1", "in_idem_d")
	r2, err := svc.RecordFailure(ctx, "t1", "sub_1", "in_idem_d")
	if err != nil {
		t.Fatalf("second RecordFailure: %v", err)
	}
	if r1.ID != r2.ID {
		t.Errorf("idempotent repeat must return same ID: %q vs %q", r1.ID, r2.ID)
	}
}

// ─── NotificationGuard ─────────────────────────────────────────────────────

// Criteria C: test mode blocks non-allowed addresses.
func TestNotificationGuard_TestMode_BlocksProduction(t *testing.T) {
	g := billingprovider.NewNotificationGuard(true,
		[]string{"example.com", "test.invalid"},
		[]string{"billing-test@internal.test"},
	)

	blocked := []string{
		"user@gmail.com",
		"real@customer.io",
		"admin@stripe.com",
	}
	for _, addr := range blocked {
		err := g.CheckAddress(addr)
		var pe billingprovider.ErrProductionEmailBlocked
		if err == nil {
			t.Errorf("address %q must be blocked in test mode (criteria C)", addr)
		} else {
			_ = pe
		}
	}
}

// Criteria C: allowed domains and explicit addresses pass in test mode.
func TestNotificationGuard_TestMode_AllowsListed(t *testing.T) {
	g := billingprovider.NewNotificationGuard(true,
		[]string{"example.com", "test.invalid"},
		[]string{"billing-test@internal.test"},
	)

	allowed := []string{
		"test@example.com",
		"x@test.invalid",
		"billing-test@internal.test",
	}
	for _, addr := range allowed {
		if err := g.CheckAddress(addr); err != nil {
			t.Errorf("address %q must be allowed: %v", addr, err)
		}
	}
}

// Criteria C: non-test mode allows all addresses (production guard not active in prod mode).
func TestNotificationGuard_NonTestMode_AllowsAll(t *testing.T) {
	g := billingprovider.NewNotificationGuard(false, nil, nil)
	addrs := []string{"user@gmail.com", "real@customer.io", "admin@example.com"}
	for _, addr := range addrs {
		if err := g.CheckAddress(addr); err != nil {
			t.Errorf("non-test mode must not block %q: %v", addr, err)
		}
	}
}
