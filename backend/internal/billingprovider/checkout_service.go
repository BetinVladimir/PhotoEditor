package billingprovider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CheckoutMode is the Stripe Checkout mode.
type CheckoutMode string

const (
	CheckoutModeSubscription CheckoutMode = "subscription"
	CheckoutModePayment      CheckoutMode = "payment"
)

// CheckoutIntentState tracks an outbox record for checkout creation.
type CheckoutIntentState string

const (
	CheckoutIntentStatePending CheckoutIntentState = "pending"
	CheckoutIntentStateCreated CheckoutIntentState = "created"
	CheckoutIntentStateFailed  CheckoutIntentState = "failed"
)

// CheckoutIntent is an outbox record requesting Stripe Checkout session creation.
// Amounts are never stored here — they are resolved server-side from the price mapping.
type CheckoutIntent struct {
	ID               uuid.UUID
	TenantID         string
	OrderID          string
	CustomerProviderID string // provider-side customer ID
	PriceProviderID  string  // server-resolved price ID; never client-supplied
	Mode             CheckoutMode
	SuccessURL       string
	CancelURL        string
	IdempotencyKey   string
	State            CheckoutIntentState
	SessionID        string // set after checkout created in provider
	ActionURL        string // set after checkout created in provider
	CreatedAt        time.Time
	ProcessedAt      *time.Time
}

// ErrIntentConflict is returned when the same order already has a pending intent.
type ErrIntentConflict struct{ OrderID string }

func (e ErrIntentConflict) Error() string { return "checkout intent already exists for order: " + e.OrderID }

// CheckoutIntentStore persists checkout intent outbox records.
type CheckoutIntentStore interface {
	GetByOrder(ctx context.Context, orderID string) (CheckoutIntent, bool, error)
	Save(ctx context.Context, intent CheckoutIntent) error
	Update(ctx context.Context, intent CheckoutIntent) error
	ListPending(ctx context.Context, limit int) ([]CheckoutIntent, error)
}

// CheckoutService manages checkout session creation via an outbox pattern.
// Prices are resolved server-side from the MappingStore, never from client input.
type CheckoutService struct {
	provider     Provider
	mappingStore MappingStore
	intentStore  CheckoutIntentStore
}

// NewCheckoutService returns a CheckoutService.
func NewCheckoutService(p Provider, m MappingStore, i CheckoutIntentStore) *CheckoutService {
	return &CheckoutService{provider: p, mappingStore: m, intentStore: i}
}

// CreateIntent writes an outbox record for a checkout session.
// priceMappingKey is the server-side key (e.g. "price:pro:monthly"); the
// resolved provider price ID is looked up from MappingStore, so no client
// amount or price ID is ever accepted directly (criteria A).
// Repeated calls with the same orderID and idempotencyKey return the existing intent (criteria B).
func (cs *CheckoutService) CreateIntent(ctx context.Context, tenantID, orderID, customerProviderID, priceMappingKey string, mode CheckoutMode, successURL, cancelURL, idempotencyKey string) (CheckoutIntent, error) {
	// Criteria B: repeat returns existing intent.
	if existing, found, err := cs.intentStore.GetByOrder(ctx, orderID); err != nil {
		return CheckoutIntent{}, fmt.Errorf("intent lookup: %w", err)
	} else if found {
		return existing, nil
	}

	// Criteria A: resolve price from server-side mapping — never from client.
	pm, found, err := cs.mappingStore.GetPriceMapping(ctx, priceMappingKey)
	if err != nil {
		return CheckoutIntent{}, fmt.Errorf("price mapping lookup: %w", err)
	}
	if !found {
		return CheckoutIntent{}, fmt.Errorf("price mapping not found for key %q", priceMappingKey)
	}
	if pm.IsTrial {
		return CheckoutIntent{}, fmt.Errorf("trial prices do not require checkout")
	}

	intent := CheckoutIntent{
		ID:                 uuid.New(),
		TenantID:           tenantID,
		OrderID:            orderID,
		CustomerProviderID: customerProviderID,
		PriceProviderID:    pm.ProviderID, // server-resolved, never client-supplied
		Mode:               mode,
		SuccessURL:         successURL,
		CancelURL:          cancelURL,
		IdempotencyKey:     idempotencyKey,
		State:              CheckoutIntentStatePending,
		CreatedAt:          time.Now(),
	}
	if err := cs.intentStore.Save(ctx, intent); err != nil {
		return CheckoutIntent{}, fmt.Errorf("save intent: %w", err)
	}
	return intent, nil
}

// ProcessIntent creates the Stripe Checkout session for a pending intent.
// It is idempotent: re-running for a created intent returns the existing URL.
// Criteria C: success URL is not proof of payment — payment is verified via webhook.
func (cs *CheckoutService) ProcessIntent(ctx context.Context, intent CheckoutIntent) (CheckoutIntent, error) {
	if intent.State == CheckoutIntentStateCreated {
		return intent, nil
	}

	key := fmt.Sprintf("checkout:intent:%s", intent.ID.String())
	hash := fmt.Sprintf("%s|%s|%s|%s", intent.CustomerProviderID, intent.PriceProviderID, intent.Mode, intent.IdempotencyKey)
	session, err := cs.provider.CreateCheckoutSession(ctx, key, hash, CreateCheckoutSessionParams{
		CustomerID: intent.CustomerProviderID,
		PriceID:    intent.PriceProviderID,
		SuccessURL: intent.SuccessURL,
		CancelURL:  intent.CancelURL,
		Metadata: map[string]string{
			"tenant_id":       intent.TenantID,
			"order_id":        intent.OrderID,
			"intent_id":       intent.ID.String(),
		},
	})
	if err != nil {
		intent.State = CheckoutIntentStateFailed
		_ = cs.intentStore.Update(ctx, intent)
		return intent, fmt.Errorf("create checkout session: %w", err)
	}

	now := time.Now()
	intent.SessionID = session.ID
	intent.ActionURL = session.URL
	intent.State = CheckoutIntentStateCreated
	intent.ProcessedAt = &now
	if err := cs.intentStore.Update(ctx, intent); err != nil {
		return intent, fmt.Errorf("update intent: %w", err)
	}
	return intent, nil
}

// GetActionURL returns the checkout URL for an order, or ("", false) if not ready.
// Criteria C: having a URL is not proof of payment.
func (cs *CheckoutService) GetActionURL(ctx context.Context, orderID string) (string, bool, error) {
	intent, found, err := cs.intentStore.GetByOrder(ctx, orderID)
	if err != nil {
		return "", false, err
	}
	if !found || intent.State != CheckoutIntentStateCreated {
		return "", false, nil
	}
	return intent.ActionURL, true, nil
}

// InMemoryCheckoutIntentStore is an in-memory CheckoutIntentStore for testing.
type InMemoryCheckoutIntentStore struct {
	byOrder map[string]CheckoutIntent // orderID → intent
	byID    map[uuid.UUID]CheckoutIntent
	pending []uuid.UUID
}

// NewInMemoryCheckoutIntentStore returns a ready-to-use in-memory store.
func NewInMemoryCheckoutIntentStore() *InMemoryCheckoutIntentStore {
	return &InMemoryCheckoutIntentStore{
		byOrder: make(map[string]CheckoutIntent),
		byID:    make(map[uuid.UUID]CheckoutIntent),
	}
}

func (s *InMemoryCheckoutIntentStore) GetByOrder(_ context.Context, orderID string) (CheckoutIntent, bool, error) {
	i, ok := s.byOrder[orderID]
	return i, ok, nil
}

func (s *InMemoryCheckoutIntentStore) Save(_ context.Context, intent CheckoutIntent) error {
	if _, exists := s.byOrder[intent.OrderID]; exists {
		return ErrIntentConflict{OrderID: intent.OrderID}
	}
	s.byOrder[intent.OrderID] = intent
	s.byID[intent.ID] = intent
	if intent.State == CheckoutIntentStatePending {
		s.pending = append(s.pending, intent.ID)
	}
	return nil
}

func (s *InMemoryCheckoutIntentStore) Update(_ context.Context, intent CheckoutIntent) error {
	if _, exists := s.byID[intent.ID]; !exists {
		return errors.New("intent not found: " + intent.ID.String())
	}
	s.byOrder[intent.OrderID] = intent
	s.byID[intent.ID] = intent
	return nil
}

func (s *InMemoryCheckoutIntentStore) ListPending(_ context.Context, limit int) ([]CheckoutIntent, error) {
	var out []CheckoutIntent
	for _, id := range s.pending {
		if i, ok := s.byID[id]; ok && i.State == CheckoutIntentStatePending {
			out = append(out, i)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
