package billingprovider

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// idempotencyRecord holds the stored result for a provider_request_key.
type idempotencyRecord struct {
	requestHash string
	objectID    string
	kind        string // "customer", "checkout", "portal", etc.
}

// FakeProvider is an in-memory implementation of Provider for testing.
// It enforces idempotency: same key + same hash → same object ID;
// same key + different hash → ErrIdempotencyConflict.
// Objects persist for the lifetime of the FakeProvider instance.
type FakeProvider struct {
	mu      sync.Mutex
	counter int

	idempotency   map[string]idempotencyRecord // key → record
	customers     map[string]Customer
	portalSess    map[string]PortalSession
	checkouts     map[string]CheckoutSession
	subscriptions map[string]Subscription
	invoices      map[string]Invoice
	refunds       map[string]Refund
}

// NewFakeProvider returns a ready-to-use FakeProvider.
func NewFakeProvider() *FakeProvider {
	return &FakeProvider{
		idempotency:   make(map[string]idempotencyRecord),
		customers:     make(map[string]Customer),
		portalSess:    make(map[string]PortalSession),
		checkouts:     make(map[string]CheckoutSession),
		subscriptions: make(map[string]Subscription),
		invoices:      make(map[string]Invoice),
		refunds:       make(map[string]Refund),
	}
}

func (f *FakeProvider) nextID(prefix string) string {
	f.counter++
	return fmt.Sprintf("%s_fake_%06d", prefix, f.counter)
}

// checkIdempotency checks and registers a provider_request_key.
// Returns (existingObjectID, alreadyExists, error).
func (f *FakeProvider) checkIdempotency(key, hash, kind string) (string, bool, error) {
	if existing, ok := f.idempotency[key]; ok {
		if existing.requestHash != hash {
			return "", false, ErrIdempotencyConflict{Key: key}
		}
		return existing.objectID, true, nil
	}
	return "", false, nil
}

func (f *FakeProvider) registerIdempotency(key, hash, objectID, kind string) {
	f.idempotency[key] = idempotencyRecord{requestHash: hash, objectID: objectID, kind: kind}
}

// CreateCustomer creates a new customer or replays the existing one for the key.
func (f *FakeProvider) CreateCustomer(_ context.Context, key, requestHash string, params CreateCustomerParams) (Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if existingID, exists, err := f.checkIdempotency(key, requestHash, "customer"); err != nil {
		return Customer{}, err
	} else if exists {
		return f.customers[existingID], nil
	}

	id := f.nextID("cus")
	c := Customer{ID: id, Email: params.Email, Metadata: params.Metadata, CreatedAt: time.Now()}
	f.customers[id] = c
	f.registerIdempotency(key, requestHash, id, "customer")
	return c, nil
}

// GetCustomer retrieves a customer by ID.
func (f *FakeProvider) GetCustomer(_ context.Context, id string) (Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.customers[id]
	if !ok {
		return Customer{}, ErrNotFound{ID: id}
	}
	return c, nil
}

// CreatePortalSession creates a new portal session or replays the existing one.
func (f *FakeProvider) CreatePortalSession(_ context.Context, key, requestHash string, params CreatePortalSessionParams) (PortalSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if existingID, exists, err := f.checkIdempotency(key, requestHash, "portal"); err != nil {
		return PortalSession{}, err
	} else if exists {
		return f.portalSess[existingID], nil
	}

	id := f.nextID("bps")
	s := PortalSession{
		ID:         id,
		CustomerID: params.CustomerID,
		URL:        "https://billing.stripe.test/portal/" + id,
		CreatedAt:  time.Now(),
	}
	f.portalSess[id] = s
	f.registerIdempotency(key, requestHash, id, "portal")
	return s, nil
}

// CreateCheckoutSession creates a checkout session or replays the existing one.
func (f *FakeProvider) CreateCheckoutSession(_ context.Context, key, requestHash string, params CreateCheckoutSessionParams) (CheckoutSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if existingID, exists, err := f.checkIdempotency(key, requestHash, "checkout"); err != nil {
		return CheckoutSession{}, err
	} else if exists {
		return f.checkouts[existingID], nil
	}

	id := f.nextID("cs")
	s := CheckoutSession{
		ID:         id,
		CustomerID: params.CustomerID,
		URL:        "https://checkout.stripe.test/" + id,
		Status:     "open",
		ExpiresAt:  time.Now().Add(24 * time.Hour),
		CreatedAt:  time.Now(),
	}
	f.checkouts[id] = s
	f.registerIdempotency(key, requestHash, id, "checkout")
	return s, nil
}

// GetCheckoutSession retrieves a checkout session by ID.
func (f *FakeProvider) GetCheckoutSession(_ context.Context, id string) (CheckoutSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.checkouts[id]
	if !ok {
		return CheckoutSession{}, ErrNotFound{ID: id}
	}
	return s, nil
}

// SetCheckoutSessionStatus allows tests to simulate session state changes.
func (f *FakeProvider) SetCheckoutSessionStatus(id, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.checkouts[id]; ok {
		s.Status = status
		f.checkouts[id] = s
	}
}

// SeedSubscription inserts a subscription directly (for test setup).
func (f *FakeProvider) SeedSubscription(sub Subscription) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscriptions[sub.ID] = sub
}

// GetSubscription retrieves a subscription by ID.
func (f *FakeProvider) GetSubscription(_ context.Context, id string) (Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.subscriptions[id]
	if !ok {
		return Subscription{}, ErrNotFound{ID: id}
	}
	return s, nil
}

// SeedInvoice inserts an invoice directly (for test setup).
func (f *FakeProvider) SeedInvoice(inv Invoice) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invoices[inv.ID] = inv
}

// GetInvoice retrieves an invoice by ID.
func (f *FakeProvider) GetInvoice(_ context.Context, id string) (Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inv, ok := f.invoices[id]
	if !ok {
		return Invoice{}, ErrNotFound{ID: id}
	}
	return inv, nil
}

// SeedRefund inserts a refund directly (for test setup).
func (f *FakeProvider) SeedRefund(ref Refund) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunds[ref.ID] = ref
}

// GetRefund retrieves a refund by ID.
func (f *FakeProvider) GetRefund(_ context.Context, id string) (Refund, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.refunds[id]
	if !ok {
		return Refund{}, ErrNotFound{ID: id}
	}
	return r, nil
}

// ParseEvent decodes a fake webhook payload. Signature is checked by
// comparing against the literal string "valid" for test scenarios;
// any other value returns an error.
func (f *FakeProvider) ParseEvent(payload []byte, signature, secret string) (Event, error) {
	if signature == "" {
		return Event{}, fmt.Errorf("STRIPE_SIGNATURE_MISSING")
	}
	// Fake: accept any non-empty signature when secret == "test_secret".
	if secret != "test_secret" && signature != "valid" {
		return Event{}, fmt.Errorf("STRIPE_SIGNATURE_INVALID")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID("evt")
	return Event{ID: id, Type: "test.event", CreatedAt: time.Now(), RawPayload: payload}, nil
}
