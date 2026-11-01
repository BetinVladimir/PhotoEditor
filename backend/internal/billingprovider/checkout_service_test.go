package billingprovider_test

import (
	"context"
	"testing"

	"photoeditor/backend/internal/billingprovider"
)

func makeCheckoutSetup(t *testing.T) (*billingprovider.CheckoutService, *billingprovider.FakeProvider, *billingprovider.InMemoryMappingStore, *billingprovider.InMemoryCheckoutIntentStore) {
	t.Helper()
	p := billingprovider.NewFakeProvider()
	ms := billingprovider.NewInMemoryMappingStore()
	is := billingprovider.NewInMemoryCheckoutIntentStore()

	// Provision a product and price so mapping is available.
	prov := billingprovider.NewProvisioner(p, ms)
	ctx := context.Background()
	_, err := prov.Run(ctx, "v1", "acct_test", []billingprovider.CatalogItem{
		{MappingKey: "plan:pro:v1", Name: "Pro"},
	}, []billingprovider.PriceItem{
		{MappingKey: "price:pro:monthly", ProductKey: "plan:pro:v1", AmountMinor: 999, Currency: "eur", Interval: "month"},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	svc := billingprovider.NewCheckoutService(p, ms, is)
	return svc, p, ms, is
}

// Criteria A: price is never taken from client — it is resolved from MappingStore.
func TestCheckoutService_ServerSidePriceOnly(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := makeCheckoutSetup(t)

	// Valid: uses server-side mapping key
	intent, err := svc.CreateIntent(ctx, "t1", "order-1", "cus_test",
		"price:pro:monthly", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-1")
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	if intent.PriceProviderID == "" {
		t.Error("PriceProviderID must be resolved from server mapping, not be empty")
	}

	// Verify that the price provider ID is from provisioning (not a client string)
	if intent.PriceProviderID == "price_custom_from_client" {
		t.Error("intent must not accept client-supplied price IDs")
	}
}

// Criteria A: unknown mapping key is rejected.
func TestCheckoutService_UnknownMappingKey_Rejected(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := makeCheckoutSetup(t)

	_, err := svc.CreateIntent(ctx, "t1", "order-2", "cus_test",
		"price:nonexistent", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-2")
	if err == nil {
		t.Error("unknown price mapping key should be rejected")
	}
}

// Criteria B: repeat CreateIntent with same orderID returns existing intent.
func TestCheckoutService_RepeatIntentSameOrder(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := makeCheckoutSetup(t)

	i1, err := svc.CreateIntent(ctx, "t1", "order-3", "cus_test",
		"price:pro:monthly", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-3")
	if err != nil {
		t.Fatalf("first intent: %v", err)
	}
	i2, err := svc.CreateIntent(ctx, "t1", "order-3", "cus_test",
		"price:pro:monthly", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-3")
	if err != nil {
		t.Fatalf("second intent: %v", err)
	}
	if i1.ID != i2.ID {
		t.Errorf("repeat intent returned different ID: %q vs %q", i1.ID, i2.ID)
	}
}

// ProcessIntent turns pending intent into a created session with URL.
func TestCheckoutService_ProcessIntent_CreatesURL(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := makeCheckoutSetup(t)

	intent, err := svc.CreateIntent(ctx, "t1", "order-4", "cus_test",
		"price:pro:monthly", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-4")
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}

	processed, err := svc.ProcessIntent(ctx, intent)
	if err != nil {
		t.Fatalf("ProcessIntent: %v", err)
	}
	if processed.State != billingprovider.CheckoutIntentStateCreated {
		t.Errorf("expected state created, got %q", processed.State)
	}
	if processed.ActionURL == "" {
		t.Error("ActionURL must be set after processing")
	}
	if processed.SessionID == "" {
		t.Error("SessionID must be set after processing")
	}
}

// ProcessIntent on an already-created intent is a no-op (idempotent).
func TestCheckoutService_ProcessIntent_Idempotent(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := makeCheckoutSetup(t)

	intent, _ := svc.CreateIntent(ctx, "t1", "order-5", "cus_test",
		"price:pro:monthly", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-5")
	p1, err := svc.ProcessIntent(ctx, intent)
	if err != nil {
		t.Fatalf("first process: %v", err)
	}
	p2, err := svc.ProcessIntent(ctx, p1)
	if err != nil {
		t.Fatalf("second process: %v", err)
	}
	if p1.SessionID != p2.SessionID {
		t.Error("re-processing should not create a new session")
	}
}

// GetActionURL returns URL only after processing.
func TestCheckoutService_GetActionURL(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := makeCheckoutSetup(t)

	// Before processing: no URL.
	intent, _ := svc.CreateIntent(ctx, "t1", "order-6", "cus_test",
		"price:pro:monthly", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-6")
	url, ready, err := svc.GetActionURL(ctx, "order-6")
	if err != nil {
		t.Fatalf("GetActionURL (before process): %v", err)
	}
	if ready {
		t.Error("action URL should not be ready before processing")
	}
	if url != "" {
		t.Errorf("expected empty URL before process, got %q", url)
	}

	// After processing: URL available.
	if _, err := svc.ProcessIntent(ctx, intent); err != nil {
		t.Fatalf("ProcessIntent: %v", err)
	}
	url, ready, err = svc.GetActionURL(ctx, "order-6")
	if err != nil {
		t.Fatalf("GetActionURL (after process): %v", err)
	}
	if !ready {
		t.Error("action URL should be ready after processing")
	}
	if url == "" {
		t.Error("action URL should be non-empty after processing")
	}
}

// Criteria C: success URL is not proof of payment — returning the URL does not
// imply any entitlement was granted; that happens via webhook verification.
func TestCheckoutService_ReturnURL_NotProofOfPayment(t *testing.T) {
	ctx := context.Background()
	svc, fp, _, _ := makeCheckoutSetup(t)

	intent, _ := svc.CreateIntent(ctx, "t1", "order-7", "cus_test",
		"price:pro:monthly", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-7")
	processed, _ := svc.ProcessIntent(ctx, intent)

	// Even with a URL, the provider session is still "open" (not "complete").
	session, err := fp.GetCheckoutSession(ctx, processed.SessionID)
	if err != nil {
		t.Fatalf("GetCheckoutSession: %v", err)
	}
	if session.Status != "open" {
		t.Errorf("new session must be 'open', not %q — return URL is not payment proof", session.Status)
	}
}

// Trial mapping key is rejected (no provider price).
func TestCheckoutService_TrialPrice_Rejected(t *testing.T) {
	ctx := context.Background()
	p := billingprovider.NewFakeProvider()
	ms := billingprovider.NewInMemoryMappingStore()
	is := billingprovider.NewInMemoryCheckoutIntentStore()

	// Provision trial item
	prov := billingprovider.NewProvisioner(p, ms)
	_, err := prov.Run(ctx, "v1", "acct_test", []billingprovider.CatalogItem{
		{MappingKey: "plan:free:v1", Name: "Free"},
	}, []billingprovider.PriceItem{
		{MappingKey: "price:free:trial", ProductKey: "plan:free:v1", IsTrial: true},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	svc := billingprovider.NewCheckoutService(p, ms, is)
	_, err = svc.CreateIntent(ctx, "t1", "order-trial", "cus_test",
		"price:free:trial", billingprovider.CheckoutModeSubscription,
		"https://app.test/ok", "https://app.test/cancel", "idp-trial")
	if err == nil {
		t.Error("trial price should be rejected for checkout")
	}
}
