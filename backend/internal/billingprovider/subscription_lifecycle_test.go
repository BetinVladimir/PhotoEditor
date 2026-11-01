package billingprovider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"photoeditor/backend/internal/billingprovider"
)

func newLifecycle() (*billingprovider.SubscriptionLifecycleService, *billingprovider.FakeProvider, *billingprovider.InMemoryFinancialSubjectStore, *billingprovider.InMemorySubscriptionLifecycleStore) {
	p := billingprovider.NewFakeProvider()
	ss := billingprovider.NewInMemoryFinancialSubjectStore()
	sl := billingprovider.NewInMemorySubscriptionLifecycleStore()
	return billingprovider.NewSubscriptionLifecycleService(p, ss, sl), p, ss, sl
}

// Criteria A: only one active subscription per billing profile.
func TestSubscriptionLifecycle_OneActive_SecondRejected(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newLifecycle()

	start := time.Now()
	_, err := svc.RecordSubscription(ctx, "t1", "sub_first", start, true, false)
	if err != nil {
		t.Fatalf("first RecordSubscription: %v", err)
	}

	_, err = svc.RecordSubscription(ctx, "t1", "sub_second", start, true, false)
	if err == nil {
		t.Error("second subscription on same tenant must be rejected (criteria A)")
	}
}

// Criteria B: MonthAnchor derived from periodStart, shared by all items.
func TestSubscriptionLifecycle_SharedMonthAnchor(t *testing.T) {
	ctx := context.Background()
	svc, _, _, sl := newLifecycle()

	// Period starts on day 15 → anchor must be 15.
	periodStart := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	_, err := svc.RecordSubscription(ctx, "t2", "sub_anchor", periodStart, true, true)
	if err != nil {
		t.Fatalf("RecordSubscription: %v", err)
	}

	profile, found, _ := sl.GetByTenant(ctx, "t2")
	if !found {
		t.Fatal("profile not found")
	}
	if profile.MonthAnchor != 15 {
		t.Errorf("MonthAnchor must be 15, got %d (criteria B: all items share anchor)", profile.MonthAnchor)
	}
}

// Criteria C: storage add-on reuses existing subscription, no new subscription.
func TestSubscriptionLifecycle_StorageAddon_NoSecondSubscription(t *testing.T) {
	ctx := context.Background()
	svc, _, _, sl := newLifecycle()

	start := time.Now()
	_, err := svc.RecordSubscription(ctx, "t3", "sub_base", start, true, false)
	if err != nil {
		t.Fatalf("RecordSubscription: %v", err)
	}

	profile, err := svc.AddStorageAddon(ctx, "t3")
	if err != nil {
		t.Fatalf("AddStorageAddon: %v", err)
	}
	if !profile.StorageEnabled {
		t.Error("storage must be enabled after add-on (criteria C)")
	}

	// Still only one profile.
	stored, _, _ := sl.GetByTenant(ctx, "t3")
	if stored.ID != profile.ID {
		t.Error("add-on must not create a new subscription profile (criteria C)")
	}
}

// Criteria C: Hosted Checkout blocked when subscription already exists.
func TestSubscriptionLifecycle_HostedCheckout_BlockedIfSubscriptionExists(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newLifecycle()

	start := time.Now()
	svc.RecordSubscription(ctx, "t4", "sub_exists", start, true, false)

	res, err := svc.CheckHostedCheckoutEligible(ctx, "t4")
	if err != nil {
		t.Fatalf("CheckHostedCheckoutEligible: %v", err)
	}
	if res.Eligible {
		t.Error("Hosted Checkout must be blocked when subscription already exists (criteria C)")
	}
}

// Criteria C: Hosted Checkout allowed when no subscription exists.
func TestSubscriptionLifecycle_HostedCheckout_AllowedWhenNoSubscription(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newLifecycle()

	res, err := svc.CheckHostedCheckoutEligible(ctx, "t5")
	if err != nil {
		t.Fatalf("CheckHostedCheckoutEligible: %v", err)
	}
	if !res.Eligible {
		t.Errorf("Hosted Checkout must be allowed when no subscription exists: reason=%q (criteria C)", res.Reason)
	}
}

// Criteria D: deleting user from transferred tenant must not close billing profile.
func TestSubscriptionLifecycle_DeleteUser_TransferredTenant_Blocked(t *testing.T) {
	ctx := context.Background()
	svc, _, ss, _ := newLifecycle()

	subj := billingprovider.FinancialSubject{
		TenantID:       "tenant-transferred",
		OwnerAccountID: "acct-new-owner",
		State:          billingprovider.FinancialSubjectTransferred,
		TransferredTo:  "acct-new-owner",
		CreatedAt:      time.Now(),
		StateChangedAt: time.Now(),
	}
	ss.Save(ctx, subj)

	// Old user (acct-old-user) tries to delete — must be blocked.
	err := svc.DeleteUserGuard(ctx, "tenant-transferred", "acct-old-user")
	var blocked billingprovider.ErrFinancialSubjectBlocked
	if !errors.As(err, &blocked) {
		t.Errorf("transferred tenant delete by non-owner must return ErrFinancialSubjectBlocked, got %v (criteria D)", err)
	}
}

// Criteria D: actual owner can trigger deletion.
func TestSubscriptionLifecycle_DeleteUser_OwnerAllowed(t *testing.T) {
	ctx := context.Background()
	svc, _, ss, _ := newLifecycle()

	subj := billingprovider.FinancialSubject{
		TenantID:       "tenant-transferred-2",
		OwnerAccountID: "acct-new-owner",
		State:          billingprovider.FinancialSubjectTransferred,
		TransferredTo:  "acct-new-owner",
		CreatedAt:      time.Now(),
		StateChangedAt: time.Now(),
	}
	ss.Save(ctx, subj)

	if err := svc.DeleteUserGuard(ctx, "tenant-transferred-2", "acct-new-owner"); err != nil {
		t.Errorf("actual owner must be allowed to trigger deletion, got %v (criteria D)", err)
	}
}

// Purchase blocked when financial subject is deleted.
func TestSubscriptionLifecycle_DeletedSubject_BlocksPurchase(t *testing.T) {
	ctx := context.Background()
	svc, _, ss, _ := newLifecycle()

	subj := billingprovider.FinancialSubject{
		TenantID:       "tenant-deleted",
		OwnerAccountID: "acct-del",
		State:          billingprovider.FinancialSubjectDeleted,
		CreatedAt:      time.Now(),
	}
	ss.Save(ctx, subj)

	_, err := svc.RecordSubscription(ctx, "tenant-deleted", "sub_blocked", time.Now(), true, false)
	if err == nil {
		t.Error("purchase must be blocked for deleted financial subject")
	}
}
