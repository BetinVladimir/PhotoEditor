// Package billingprovider defines the interface for billing payment provider
// operations and provides a fake in-memory implementation for testing.
package billingprovider

import (
	"context"
	"time"
)

// Customer is a billing provider customer record.
type Customer struct {
	ID        string
	Email     string
	Metadata  map[string]string
	CreatedAt time.Time
}

// CreateCustomerParams holds parameters for customer creation.
type CreateCustomerParams struct {
	Email    string
	Metadata map[string]string
}

// PortalSession is a billing provider customer portal session.
type PortalSession struct {
	ID         string
	CustomerID string
	URL        string
	CreatedAt  time.Time
}

// CreatePortalSessionParams holds parameters for creating a portal session.
type CreatePortalSessionParams struct {
	CustomerID     string
	ReturnURL      string
	ConfigurationID string
}

// CheckoutSession is a billing provider hosted checkout session.
type CheckoutSession struct {
	ID         string
	CustomerID string
	URL        string
	Status     string // "open", "complete", "expired"
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// CreateCheckoutSessionParams holds parameters for creating a checkout session.
type CreateCheckoutSessionParams struct {
	CustomerID string
	PriceID    string
	SuccessURL string
	CancelURL  string
	Metadata   map[string]string
}

// Subscription is a billing provider subscription record.
type Subscription struct {
	ID         string
	CustomerID string
	PriceID    string
	Status     string // "active", "past_due", "canceled", "trialing"
	PeriodEnd  time.Time
	CreatedAt  time.Time
}

// Invoice is a billing provider invoice record.
type Invoice struct {
	ID             string
	CustomerID     string
	SubscriptionID string
	Status         string // "draft", "open", "paid", "void", "uncollectible"
	AmountDue      int64  // minor units (e.g. cents)
	Currency       string
	CreatedAt      time.Time
}

// Refund is a billing provider refund record.
type Refund struct {
	ID        string
	ChargeID  string
	Amount    int64 // minor units
	Status    string // "pending", "succeeded", "failed"
	CreatedAt time.Time
}

// Event is a billing provider webhook event.
type Event struct {
	ID        string
	Type      string
	AccountID string
	CreatedAt time.Time
	// RawPayload holds the raw bytes for further decoding.
	RawPayload []byte
}

// ErrNotFound is returned when an object does not exist.
type ErrNotFound struct{ ID string }

func (e ErrNotFound) Error() string { return "billing provider object not found: " + e.ID }

// ErrIdempotencyConflict is returned when the same key is reused with a
// different request hash.
type ErrIdempotencyConflict struct{ Key string }

func (e ErrIdempotencyConflict) Error() string {
	return "billing provider idempotency conflict for key: " + e.Key
}

// ErrUnavailable is returned by the stub adapter when no provider is configured.
type ErrUnavailable struct{}

func (ErrUnavailable) Error() string { return "BILLING_UNAVAILABLE" }

// Provider is the interface for external payment provider operations.
// All mutating calls accept a providerRequestKey for idempotency:
// identical key + identical hash → same response;
// identical key + different hash → ErrIdempotencyConflict.
// A timeout after a successful call must be recovered by reading the
// object, not by retrying with a new key (criteria D).
type Provider interface {
	// Customer operations
	CreateCustomer(ctx context.Context, key, requestHash string, params CreateCustomerParams) (Customer, error)
	GetCustomer(ctx context.Context, id string) (Customer, error)

	// Portal session
	CreatePortalSession(ctx context.Context, key, requestHash string, params CreatePortalSessionParams) (PortalSession, error)

	// Checkout
	CreateCheckoutSession(ctx context.Context, key, requestHash string, params CreateCheckoutSessionParams) (CheckoutSession, error)
	GetCheckoutSession(ctx context.Context, id string) (CheckoutSession, error)

	// Subscription
	GetSubscription(ctx context.Context, id string) (Subscription, error)

	// Invoice
	GetInvoice(ctx context.Context, id string) (Invoice, error)

	// Refund
	GetRefund(ctx context.Context, id string) (Refund, error)

	// Event parsing (webhook ingestion)
	ParseEvent(payload []byte, signature, secret string) (Event, error)
}

// UnavailableProvider is a stub adapter that returns ErrUnavailable for every
// call. It is used when BILLING_STRIPE_ENABLED is false or configuration is
// incomplete.
type UnavailableProvider struct{}

func (UnavailableProvider) CreateCustomer(_ context.Context, _, _ string, _ CreateCustomerParams) (Customer, error) {
	return Customer{}, ErrUnavailable{}
}
func (UnavailableProvider) GetCustomer(_ context.Context, _ string) (Customer, error) {
	return Customer{}, ErrUnavailable{}
}
func (UnavailableProvider) CreatePortalSession(_ context.Context, _, _ string, _ CreatePortalSessionParams) (PortalSession, error) {
	return PortalSession{}, ErrUnavailable{}
}
func (UnavailableProvider) CreateCheckoutSession(_ context.Context, _, _ string, _ CreateCheckoutSessionParams) (CheckoutSession, error) {
	return CheckoutSession{}, ErrUnavailable{}
}
func (UnavailableProvider) GetCheckoutSession(_ context.Context, _ string) (CheckoutSession, error) {
	return CheckoutSession{}, ErrUnavailable{}
}
func (UnavailableProvider) GetSubscription(_ context.Context, _ string) (Subscription, error) {
	return Subscription{}, ErrUnavailable{}
}
func (UnavailableProvider) GetInvoice(_ context.Context, _ string) (Invoice, error) {
	return Invoice{}, ErrUnavailable{}
}
func (UnavailableProvider) GetRefund(_ context.Context, _ string) (Refund, error) {
	return Refund{}, ErrUnavailable{}
}
func (UnavailableProvider) ParseEvent(_ []byte, _, _ string) (Event, error) {
	return Event{}, ErrUnavailable{}
}
