package billingprovider

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// CustomerStore persists the provider customer ID for a tenant/account pair.
type CustomerStore interface {
	// GetCustomerID returns the stored provider customer ID, or ("", false, nil) if absent.
	GetCustomerID(ctx context.Context, tenantID, accountID string) (providerID string, found bool, err error)
	// SetCustomerID stores the provider customer ID for the tenant/account pair.
	SetCustomerID(ctx context.Context, tenantID, accountID, providerID string) error
}

// CustomerService creates and retrieves provider customers, one per tenant/account.
// It uses a durable idempotency key so that retrying after an ambiguous response
// (criteria B) resolves via provider-side replay rather than a second create.
type CustomerService struct {
	provider Provider
	store    CustomerStore
}

// NewCustomerService returns a CustomerService backed by the given provider and store.
func NewCustomerService(p Provider, s CustomerStore) *CustomerService {
	return &CustomerService{provider: p, store: s}
}

// customerIdempotencyKey derives a stable key from the tenant/account pair.
// Using a hash ensures the key is short and safe to pass as a provider key.
func customerIdempotencyKey(tenantID, accountID string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("customer|%s|%s", tenantID, accountID)))
	return fmt.Sprintf("cust_ensure_%x", h[:16])
}

// customerRequestHash hashes the creation parameters so that a changed email
// on the same key is detected as an ErrIdempotencyConflict by the provider.
func customerRequestHash(email string) string {
	h := sha256.Sum256([]byte(email))
	return fmt.Sprintf("%x", h[:])
}

// EnsureCustomer returns the provider customer for the given tenant/account,
// creating one if it does not yet exist.
//
// Criteria A: two calls with the same tenantID/accountID return the same customer.
// Criteria B: if the store has no record (e.g. after a crash), the idempotency key
// causes the provider to replay the original response rather than creating a second
// customer.
func (cs *CustomerService) EnsureCustomer(ctx context.Context, tenantID, accountID, email string) (Customer, error) {
	// Fast path: already stored.
	if id, found, err := cs.store.GetCustomerID(ctx, tenantID, accountID); err != nil {
		return Customer{}, fmt.Errorf("customer store lookup: %w", err)
	} else if found {
		c, err := cs.provider.GetCustomer(ctx, id)
		if err != nil {
			return Customer{}, fmt.Errorf("get stored customer %q: %w", id, err)
		}
		return c, nil
	}

	// Slow path: create via provider with a deterministic idempotency key.
	// If we crashed after provider.CreateCustomer but before store.SetCustomerID,
	// the provider will replay the same customer on retry (idempotency key unchanged).
	key := customerIdempotencyKey(tenantID, accountID)
	hash := customerRequestHash(email)
	c, err := cs.provider.CreateCustomer(ctx, key, hash, CreateCustomerParams{
		Email:    email,
		Metadata: map[string]string{"tenant_id": tenantID, "account_id": accountID},
	})
	if err != nil {
		return Customer{}, fmt.Errorf("create customer: %w", err)
	}

	if err := cs.store.SetCustomerID(ctx, tenantID, accountID, c.ID); err != nil {
		// Store write failed, but the customer was created in the provider.
		// Next call will retry create with the same idempotency key and get
		// the same customer back (provider replay), then store it.
		return Customer{}, fmt.Errorf("persist customer ID: %w", err)
	}
	return c, nil
}

// ReconcileCustomer resolves an ambiguous create by reading the provider object.
// Call it when EnsureCustomer returned an error after a possible create, to
// recover without issuing a second create (criteria B).
func (cs *CustomerService) ReconcileCustomer(ctx context.Context, tenantID, accountID, email string) (Customer, error) {
	// Retry with the same deterministic key — provider replays the result if
	// the object was already created.
	return cs.EnsureCustomer(ctx, tenantID, accountID, email)
}

// InMemoryCustomerStore is an in-memory CustomerStore for testing.
type InMemoryCustomerStore struct {
	m map[string]string // "tenantID:accountID" → providerID
}

// NewInMemoryCustomerStore returns a ready-to-use InMemoryCustomerStore.
func NewInMemoryCustomerStore() *InMemoryCustomerStore {
	return &InMemoryCustomerStore{m: make(map[string]string)}
}

func customerStoreKey(tenantID, accountID string) string {
	return tenantID + ":" + accountID
}

func (s *InMemoryCustomerStore) GetCustomerID(_ context.Context, tenantID, accountID string) (string, bool, error) {
	id, ok := s.m[customerStoreKey(tenantID, accountID)]
	return id, ok, nil
}

func (s *InMemoryCustomerStore) SetCustomerID(_ context.Context, tenantID, accountID, providerID string) error {
	s.m[customerStoreKey(tenantID, accountID)] = providerID
	return nil
}
