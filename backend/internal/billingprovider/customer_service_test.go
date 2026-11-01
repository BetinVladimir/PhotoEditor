package billingprovider_test

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/billingprovider"
)

func newCustomerService() (*billingprovider.CustomerService, *billingprovider.FakeProvider, *billingprovider.InMemoryCustomerStore) {
	p := billingprovider.NewFakeProvider()
	s := billingprovider.NewInMemoryCustomerStore()
	return billingprovider.NewCustomerService(p, s), p, s
}

// Criteria A: two calls with the same tenant/account return the same customer ID.
func TestCustomerService_DoubleCall_SameCustomer(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newCustomerService()

	c1, err := svc.EnsureCustomer(ctx, "tenant-1", "acct-1", "user@example.test")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	c2, err := svc.EnsureCustomer(ctx, "tenant-1", "acct-1", "user@example.test")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if c1.ID != c2.ID {
		t.Errorf("double call returned different customers: %q vs %q", c1.ID, c2.ID)
	}
}

// Criteria A: different tenant/account pairs produce different customers.
func TestCustomerService_DifferentTenants_DifferentCustomers(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newCustomerService()

	c1, err := svc.EnsureCustomer(ctx, "tenant-A", "acct-A", "a@example.test")
	if err != nil {
		t.Fatalf("tenant A: %v", err)
	}
	c2, err := svc.EnsureCustomer(ctx, "tenant-B", "acct-B", "b@example.test")
	if err != nil {
		t.Fatalf("tenant B: %v", err)
	}
	if c1.ID == c2.ID {
		t.Error("different tenants must produce different customers")
	}
}

// Criteria B: if the store is cleared (simulates crash-after-create), the next
// EnsureCustomer call uses the provider's idempotency replay instead of creating
// a new customer.
func TestCustomerService_Reconciliation_NoSecondCreate(t *testing.T) {
	ctx := context.Background()
	p := billingprovider.NewFakeProvider()
	store := billingprovider.NewInMemoryCustomerStore()
	svc := billingprovider.NewCustomerService(p, store)

	// First call succeeds and stores the ID.
	c1, err := svc.EnsureCustomer(ctx, "tenant-crash", "acct-crash", "crash@example.test")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Simulate crash: clear the store entry but leave the provider object.
	clearStore := billingprovider.NewInMemoryCustomerStore()
	svc2 := billingprovider.NewCustomerService(p, clearStore)

	// Second call on the blank store triggers a provider create with the same
	// deterministic key — provider replays the same customer (idempotency).
	c2, err := svc2.EnsureCustomer(ctx, "tenant-crash", "acct-crash", "crash@example.test")
	if err != nil {
		t.Fatalf("reconciliation call: %v", err)
	}
	if c1.ID != c2.ID {
		t.Errorf("reconciliation produced a new customer: %q vs %q — must reuse original", c1.ID, c2.ID)
	}
}

// ReconcileCustomer delegates to EnsureCustomer and returns the same object.
func TestCustomerService_ReconcileCustomer(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newCustomerService()

	c1, _ := svc.EnsureCustomer(ctx, "t1", "a1", "x@test.test")
	c2, err := svc.ReconcileCustomer(ctx, "t1", "a1", "x@test.test")
	if err != nil {
		t.Fatalf("ReconcileCustomer: %v", err)
	}
	if c1.ID != c2.ID {
		t.Errorf("reconcile returned different customer: %q vs %q", c1.ID, c2.ID)
	}
}

// GetCustomer can resolve a customer by provider ID after EnsureCustomer.
func TestCustomerService_GetCustomer_AfterEnsure(t *testing.T) {
	ctx := context.Background()
	svc, fp, _ := newCustomerService()

	c, err := svc.EnsureCustomer(ctx, "t1", "a1", "get@test.test")
	if err != nil {
		t.Fatalf("EnsureCustomer: %v", err)
	}
	fetched, err := fp.GetCustomer(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCustomer: %v", err)
	}
	if fetched.Email != "get@test.test" {
		t.Errorf("email mismatch: %q", fetched.Email)
	}
}

// EnsureCustomer via UnavailableProvider returns ErrUnavailable.
func TestCustomerService_UnavailableProvider(t *testing.T) {
	ctx := context.Background()
	svc := billingprovider.NewCustomerService(billingprovider.UnavailableProvider{}, billingprovider.NewInMemoryCustomerStore())
	_, err := svc.EnsureCustomer(ctx, "t1", "a1", "x@test.test")
	if err == nil {
		t.Fatal("expected error from UnavailableProvider")
	}
	var u billingprovider.ErrUnavailable
	if !errors.As(err, &u) {
		t.Errorf("expected ErrUnavailable wrapped, got %T: %v", err, err)
	}
}
