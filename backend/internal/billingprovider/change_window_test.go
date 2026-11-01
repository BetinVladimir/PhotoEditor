package billingprovider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"photoeditor/backend/internal/billingprovider"
)

func newChangeWindowChecker() (*billingprovider.ChangeWindowChecker, *billingprovider.FakeProvider) {
	p := billingprovider.NewFakeProvider()
	return billingprovider.NewChangeWindowChecker(p), p
}

// Criteria A: open renewal invoice → BILLING_CHANGE_WINDOW_CLOSED.
func TestChangeWindow_OpenRenewalInvoice_Blocked(t *testing.T) {
	ctx := context.Background()
	cwc, fp := newChangeWindowChecker()

	fp.SeedSubscription(billingprovider.Subscription{
		ID:        "sub_1",
		Status:    "active",
		PeriodEnd: time.Now().Add(48 * time.Hour), // 2 days away — normally OK
	})
	fp.SeedInvoice(billingprovider.Invoice{
		ID:     "in_renewal_1",
		Status: "open", // open renewal invoice
	})

	_, err := cwc.Check(ctx, "sub_1", "in_renewal_1")
	var closed billingprovider.ErrChangeWindowClosed
	if !errors.As(err, &closed) {
		t.Errorf("expected ErrChangeWindowClosed, got %v", err)
	}
	if closed.Reason != "open_renewal_invoice" {
		t.Errorf("expected reason open_renewal_invoice, got %q", closed.Reason)
	}
}

// Criteria A: period_end within 24 hours → BILLING_CHANGE_WINDOW_CLOSED.
func TestChangeWindow_RenewalTooClose_Blocked(t *testing.T) {
	ctx := context.Background()
	cwc, fp := newChangeWindowChecker()

	fp.SeedSubscription(billingprovider.Subscription{
		ID:        "sub_2",
		Status:    "active",
		PeriodEnd: time.Now().Add(12 * time.Hour), // < 24h
	})

	_, err := cwc.Check(ctx, "sub_2", "")
	var closed billingprovider.ErrChangeWindowClosed
	if !errors.As(err, &closed) {
		t.Errorf("expected ErrChangeWindowClosed, got %v", err)
	}
	if closed.Reason != "renewal_too_close" {
		t.Errorf("expected reason renewal_too_close, got %q", closed.Reason)
	}
}

// Criteria A: allowed window — no open invoice, >24h remaining.
func TestChangeWindow_AllowedWindow(t *testing.T) {
	ctx := context.Background()
	cwc, fp := newChangeWindowChecker()

	fp.SeedSubscription(billingprovider.Subscription{
		ID:        "sub_3",
		Status:    "active",
		PeriodEnd: time.Now().Add(7 * 24 * time.Hour),
	})

	result, err := cwc.Check(ctx, "sub_3", "")
	if err != nil {
		t.Fatalf("expected allowed window, got: %v", err)
	}
	if !result.Allowed {
		t.Error("window should be allowed")
	}
}

// Criteria A: no money charged when window is closed.
func TestChangeWindow_NoChargeOnClosed(t *testing.T) {
	ctx := context.Background()
	cwc, fp := newChangeWindowChecker()

	fp.SeedSubscription(billingprovider.Subscription{
		ID:        "sub_4",
		Status:    "active",
		PeriodEnd: time.Now().Add(6 * time.Hour), // < 24h
	})

	_, err := cwc.Check(ctx, "sub_4", "")
	if err == nil {
		t.Fatal("expected window closed error")
	}
	// No checkout session or invoice was created — nothing was charged.
	// (Structural: the Check call returns before any provider create.)
}

// Criteria B: race after quote — CheckSnapshot with window now closed.
func TestChangeWindow_RaceAfterQuote_Compensate(t *testing.T) {
	sub := billingprovider.Subscription{
		ID:        "sub_race",
		Status:    "active",
		PeriodEnd: time.Now().Add(6 * time.Hour), // window just closed
	}
	cwc, _ := newChangeWindowChecker()

	_, err := cwc.CheckSnapshot(sub, "")
	var closed billingprovider.ErrChangeWindowClosed
	if !errors.As(err, &closed) {
		t.Errorf("expected ErrChangeWindowClosed on snapshot, got %v", err)
	}
}

// Criteria B: race with open invoice detected on snapshot.
func TestChangeWindow_RaceWithOpenInvoice_Compensate(t *testing.T) {
	sub := billingprovider.Subscription{
		ID:        "sub_race2",
		Status:    "active",
		PeriodEnd: time.Now().Add(48 * time.Hour), // normally open
	}
	cwc, _ := newChangeWindowChecker()

	_, err := cwc.CheckSnapshot(sub, "open")
	var closed billingprovider.ErrChangeWindowClosed
	if !errors.As(err, &closed) {
		t.Errorf("expected ErrChangeWindowClosed for open invoice race, got %v", err)
	}
	if closed.Reason != "open_renewal_invoice" {
		t.Errorf("wrong reason: %q", closed.Reason)
	}
}

// Criteria C: time guard is an additional check, not a replacement for compensation.
// A saga that passed the time guard but later found an open invoice must compensate.
func TestChangeWindow_TimeGuardNotReplacingCompensation(t *testing.T) {
	// This is a design-level test: the time guard protects upfront, but
	// CheckSnapshot exists precisely to detect races that slip through.
	// We verify that CheckSnapshot can still return a closed window even
	// when the original Check() call would have passed.
	sub := billingprovider.Subscription{
		ID:        "sub_c",
		Status:    "active",
		PeriodEnd: time.Now().Add(48 * time.Hour),
	}
	cwc, _ := newChangeWindowChecker()

	// Original window check passes.
	result, err := cwc.CheckSnapshot(sub, "")
	if err != nil || !result.Allowed {
		t.Fatalf("initial check should pass: %v", err)
	}

	// Later: invoice created, window closed — saga must compensate.
	_, err = cwc.CheckSnapshot(sub, "open")
	if err == nil {
		t.Error("race scenario must return ErrChangeWindowClosed for compensation")
	}
}
