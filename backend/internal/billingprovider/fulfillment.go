package billingprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// GrantState tracks the fulfillment state for a purchase event.
type GrantState string

const (
	GrantStatePending   GrantState = "pending"
	GrantStateApplied   GrantState = "applied"
	GrantStateConflict  GrantState = "conflict" // duplicate event, no second grant
)

// Grant is a persisted fulfillment record for one purchase.
// One purchase event → exactly one Grant (criteria A).
type Grant struct {
	ID             uuid.UUID
	TenantID       string
	OrderID        string
	InvoiceID      string // provider invoice ID; set for recurring purchases
	LineItemID     string // invoice line item ID; set for recurring
	PeriodStart    time.Time
	PeriodEnd      time.Time
	PlanKey        string     // server-side plan identifier
	State          GrantState
	GrantedAt      time.Time
}

// GrantStore persists grant records.
type GrantStore interface {
	// GetByOrder returns the grant for an order, if any.
	GetByOrder(ctx context.Context, orderID string) (Grant, bool, error)
	// GetByInvoice returns the grant for an invoice, if any.
	GetByInvoice(ctx context.Context, invoiceID string) (Grant, bool, error)
	// Save writes a new grant; must fail if a grant already exists for the order.
	Save(ctx context.Context, g Grant) error
	// Update persists state changes.
	Update(ctx context.Context, g Grant) error
}

// FulfillmentService applies grants once a payment is verified.
// All paths must be idempotent: multiple events for the same purchase result
// in at most one grant (criteria A).
type FulfillmentService struct {
	provider   Provider
	grantStore GrantStore
}

// NewFulfillmentService returns a FulfillmentService.
func NewFulfillmentService(p Provider, gs GrantStore) *FulfillmentService {
	return &FulfillmentService{provider: p, grantStore: gs}
}

// FulfillOneTime verifies a one-time payment and issues a grant.
// It reads the provider object authoritatively before granting (not just the event).
// Criteria C: if we crashed between payment-confirmed and grant-saved, restarting
// re-reads the checkout session and finds it "complete", then replays the grant.
func (fs *FulfillmentService) FulfillOneTime(ctx context.Context, tenantID, orderID, sessionID, planKey string) (Grant, error) {
	// Idempotency: already fulfilled.
	if existing, found, err := fs.grantStore.GetByOrder(ctx, orderID); err != nil {
		return Grant{}, fmt.Errorf("grant lookup: %w", err)
	} else if found {
		return existing, nil
	}

	// Authoritative check: read the actual provider object, not just the event.
	session, err := fs.provider.GetCheckoutSession(ctx, sessionID)
	if err != nil {
		return Grant{}, fmt.Errorf("get checkout session: %w", err)
	}
	if session.Status != "complete" {
		return Grant{}, fmt.Errorf("checkout session not complete (status: %q)", session.Status)
	}

	g := Grant{
		ID:        uuid.New(),
		TenantID:  tenantID,
		OrderID:   orderID,
		PlanKey:   planKey,
		State:     GrantStateApplied,
		GrantedAt: time.Now(),
	}
	if err := fs.grantStore.Save(ctx, g); err != nil {
		return Grant{}, fmt.Errorf("save grant: %w", err)
	}
	return g, nil
}

// FulfillRecurring verifies a recurring invoice payment and issues a grant
// tied to the invoice line item and period.
// Criteria B: the grant is bound to the specific invoice/line/period.
// Criteria A: calling twice for the same invoiceID returns the existing grant.
func (fs *FulfillmentService) FulfillRecurring(ctx context.Context, tenantID, orderID, invoiceID, lineItemID, planKey string, periodStart, periodEnd time.Time) (Grant, error) {
	// Idempotency by invoiceID.
	if existing, found, err := fs.grantStore.GetByInvoice(ctx, invoiceID); err != nil {
		return Grant{}, fmt.Errorf("grant lookup by invoice: %w", err)
	} else if found {
		return existing, nil
	}

	// Authoritative check: read the invoice from provider.
	invoice, err := fs.provider.GetInvoice(ctx, invoiceID)
	if err != nil {
		return Grant{}, fmt.Errorf("get invoice: %w", err)
	}
	if invoice.Status != "paid" {
		return Grant{}, fmt.Errorf("invoice not paid (status: %q)", invoice.Status)
	}

	g := Grant{
		ID:          uuid.New(),
		TenantID:    tenantID,
		OrderID:     orderID,
		InvoiceID:   invoiceID,
		LineItemID:  lineItemID,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		PlanKey:     planKey,
		State:       GrantStateApplied,
		GrantedAt:   time.Now(),
	}
	if err := fs.grantStore.Save(ctx, g); err != nil {
		return Grant{}, fmt.Errorf("save recurring grant: %w", err)
	}
	return g, nil
}

// InMemoryGrantStore is an in-memory GrantStore for testing.
type InMemoryGrantStore struct {
	byOrder   map[string]Grant
	byInvoice map[string]Grant
}

// NewInMemoryGrantStore returns a ready-to-use in-memory grant store.
func NewInMemoryGrantStore() *InMemoryGrantStore {
	return &InMemoryGrantStore{
		byOrder:   make(map[string]Grant),
		byInvoice: make(map[string]Grant),
	}
}

func (s *InMemoryGrantStore) GetByOrder(_ context.Context, orderID string) (Grant, bool, error) {
	g, ok := s.byOrder[orderID]
	return g, ok, nil
}

func (s *InMemoryGrantStore) GetByInvoice(_ context.Context, invoiceID string) (Grant, bool, error) {
	g, ok := s.byInvoice[invoiceID]
	return g, ok, nil
}

func (s *InMemoryGrantStore) Save(_ context.Context, g Grant) error {
	if _, exists := s.byOrder[g.OrderID]; exists {
		return fmt.Errorf("grant already exists for order %s", g.OrderID)
	}
	s.byOrder[g.OrderID] = g
	if g.InvoiceID != "" {
		s.byInvoice[g.InvoiceID] = g
	}
	return nil
}

func (s *InMemoryGrantStore) Update(_ context.Context, g Grant) error {
	s.byOrder[g.OrderID] = g
	if g.InvoiceID != "" {
		s.byInvoice[g.InvoiceID] = g
	}
	return nil
}
