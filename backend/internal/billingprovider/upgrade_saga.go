package billingprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// UpgradeSagaState is the state machine for an upgrade.
// Transitions: quoted → payment_pending → paid → provider_price_applying
//
//	→ entitlement_applying → applied
//	                       → compensating → compensated | manual_review
type UpgradeSagaState string

const (
	UpgradeSagaStateQuoted              UpgradeSagaState = "quoted"
	UpgradeSagaStatePaymentPending      UpgradeSagaState = "payment_pending"
	UpgradeSagaStatePaid                UpgradeSagaState = "paid"
	UpgradeSagaStatePriceApplying       UpgradeSagaState = "provider_price_applying"
	UpgradeSagaStateEntitlementApplying UpgradeSagaState = "entitlement_applying"
	UpgradeSagaStateApplied             UpgradeSagaState = "applied"
	UpgradeSagaStateCompensating        UpgradeSagaState = "compensating"
	UpgradeSagaStateCompensated         UpgradeSagaState = "compensated"
	UpgradeSagaStateManualReview        UpgradeSagaState = "manual_review"
)

// UpgradeSaga is the persisted state for one upgrade attempt.
type UpgradeSaga struct {
	ID             uuid.UUID
	TenantID       string
	OrderID        string
	FromPlanKey    string
	ToPlanKey      string
	NewPriceProviderID string // server-resolved provider price ID
	InvoiceID      string  // set after payment confirmed
	State          UpgradeSagaState
	CompensationReason string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// UpgradeSagaStore persists upgrade saga records.
type UpgradeSagaStore interface {
	GetByOrder(ctx context.Context, orderID string) (UpgradeSaga, bool, error)
	Save(ctx context.Context, saga UpgradeSaga) error
	Update(ctx context.Context, saga UpgradeSaga) error
	// GetOpenForTenant returns any non-terminal saga for the tenant.
	GetOpenForTenant(ctx context.Context, tenantID string) (UpgradeSaga, bool, error)
}

// UpgradeSagaService manages the upgrade state machine.
type UpgradeSagaService struct {
	provider  Provider
	sagaStore UpgradeSagaStore
}

// NewUpgradeSagaService returns an UpgradeSagaService.
func NewUpgradeSagaService(p Provider, ss UpgradeSagaStore) *UpgradeSagaService {
	return &UpgradeSagaService{provider: p, sagaStore: ss}
}

// CreateUpgrade creates a new saga in the "quoted" state.
// Criteria D: returns error if a previous upgrade is still open.
func (us *UpgradeSagaService) CreateUpgrade(ctx context.Context, tenantID, orderID, fromPlanKey, toPlanKey, newPriceProviderID string) (UpgradeSaga, error) {
	// Criteria D: close any previous upgrade before starting a new one.
	if open, found, err := us.sagaStore.GetOpenForTenant(ctx, tenantID); err != nil {
		return UpgradeSaga{}, fmt.Errorf("open saga check: %w", err)
	} else if found {
		return UpgradeSaga{}, fmt.Errorf("BILLING_UPGRADE_CONFLICT: existing open upgrade %q must be closed first", open.ID)
	}

	saga := UpgradeSaga{
		ID:                 uuid.New(),
		TenantID:           tenantID,
		OrderID:            orderID,
		FromPlanKey:        fromPlanKey,
		ToPlanKey:          toPlanKey,
		NewPriceProviderID: newPriceProviderID,
		State:              UpgradeSagaStateQuoted,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := us.sagaStore.Save(ctx, saga); err != nil {
		return UpgradeSaga{}, fmt.Errorf("save saga: %w", err)
	}
	return saga, nil
}

// AdvanceToPaymentPending transitions quoted → payment_pending.
func (us *UpgradeSagaService) AdvanceToPaymentPending(ctx context.Context, orderID string) (UpgradeSaga, error) {
	return us.transition(ctx, orderID, UpgradeSagaStateQuoted, UpgradeSagaStatePaymentPending)
}

// MarkPaid transitions payment_pending → paid after invoice confirmed.
// Criteria B: once paid, the saga stays "paid/applying" and does not ask for payment again.
func (us *UpgradeSagaService) MarkPaid(ctx context.Context, orderID, invoiceID string) (UpgradeSaga, error) {
	saga, found, err := us.sagaStore.GetByOrder(ctx, orderID)
	if err != nil || !found {
		return UpgradeSaga{}, fmt.Errorf("saga not found for order %q: %w", orderID, err)
	}
	if saga.State != UpgradeSagaStatePaymentPending {
		return saga, nil // already advanced — idempotent
	}

	// Authoritative invoice check.
	inv, err := us.provider.GetInvoice(ctx, invoiceID)
	if err != nil {
		return UpgradeSaga{}, fmt.Errorf("get invoice: %w", err)
	}
	if inv.Status != "paid" {
		return UpgradeSaga{}, fmt.Errorf("invoice not paid (status: %q)", inv.Status)
	}

	saga.InvoiceID = invoiceID
	saga.State = UpgradeSagaStatePaid
	saga.UpdatedAt = time.Now()
	if err := us.sagaStore.Update(ctx, saga); err != nil {
		return UpgradeSaga{}, fmt.Errorf("update saga to paid: %w", err)
	}
	return saga, nil
}

// AdvanceToPriceApplying transitions paid → provider_price_applying.
func (us *UpgradeSagaService) AdvanceToPriceApplying(ctx context.Context, orderID string) (UpgradeSaga, error) {
	return us.transition(ctx, orderID, UpgradeSagaStatePaid, UpgradeSagaStatePriceApplying)
}

// AdvanceToEntitlementApplying transitions provider_price_applying → entitlement_applying.
func (us *UpgradeSagaService) AdvanceToEntitlementApplying(ctx context.Context, orderID string) (UpgradeSaga, error) {
	return us.transition(ctx, orderID, UpgradeSagaStatePriceApplying, UpgradeSagaStateEntitlementApplying)
}

// MarkApplied transitions entitlement_applying → applied.
func (us *UpgradeSagaService) MarkApplied(ctx context.Context, orderID string) (UpgradeSaga, error) {
	return us.transition(ctx, orderID, UpgradeSagaStateEntitlementApplying, UpgradeSagaStateApplied)
}

// StartCompensation transitions to compensating state with a reason.
// Criteria C: called when the upgrade cannot be applied after payment.
func (us *UpgradeSagaService) StartCompensation(ctx context.Context, orderID, reason string) (UpgradeSaga, error) {
	saga, found, err := us.sagaStore.GetByOrder(ctx, orderID)
	if err != nil || !found {
		return UpgradeSaga{}, fmt.Errorf("saga not found for order %q: %w", orderID, err)
	}
	saga.State = UpgradeSagaStateCompensating
	saga.CompensationReason = reason
	saga.UpdatedAt = time.Now()
	if err := us.sagaStore.Update(ctx, saga); err != nil {
		return UpgradeSaga{}, fmt.Errorf("update saga to compensating: %w", err)
	}
	return saga, nil
}

// MarkCompensated transitions compensating → compensated.
func (us *UpgradeSagaService) MarkCompensated(ctx context.Context, orderID string) (UpgradeSaga, error) {
	return us.transition(ctx, orderID, UpgradeSagaStateCompensating, UpgradeSagaStateCompensated)
}

// MarkManualReview transitions compensating → manual_review.
func (us *UpgradeSagaService) MarkManualReview(ctx context.Context, orderID string) (UpgradeSaga, error) {
	return us.transition(ctx, orderID, UpgradeSagaStateCompensating, UpgradeSagaStateManualReview)
}

func (us *UpgradeSagaService) transition(ctx context.Context, orderID string, from, to UpgradeSagaState) (UpgradeSaga, error) {
	saga, found, err := us.sagaStore.GetByOrder(ctx, orderID)
	if err != nil || !found {
		return UpgradeSaga{}, fmt.Errorf("saga not found for order %q: %w", orderID, err)
	}
	if saga.State == to {
		return saga, nil // idempotent
	}
	if saga.State != from {
		return saga, fmt.Errorf("saga in state %q, expected %q for transition to %q", saga.State, from, to)
	}
	saga.State = to
	saga.UpdatedAt = time.Now()
	if err := us.sagaStore.Update(ctx, saga); err != nil {
		return UpgradeSaga{}, fmt.Errorf("update saga to %q: %w", to, err)
	}
	return saga, nil
}

// IsTerminal returns true if the saga is in a terminal state.
func (saga UpgradeSaga) IsTerminal() bool {
	switch saga.State {
	case UpgradeSagaStateApplied, UpgradeSagaStateCompensated, UpgradeSagaStateManualReview:
		return true
	}
	return false
}

// InMemoryUpgradeSagaStore is an in-memory UpgradeSagaStore for testing.
type InMemoryUpgradeSagaStore struct {
	byOrder  map[string]UpgradeSaga
	byTenant map[string]string // tenantID → orderID of open saga
}

// NewInMemoryUpgradeSagaStore returns a ready-to-use store.
func NewInMemoryUpgradeSagaStore() *InMemoryUpgradeSagaStore {
	return &InMemoryUpgradeSagaStore{
		byOrder:  make(map[string]UpgradeSaga),
		byTenant: make(map[string]string),
	}
}

func (s *InMemoryUpgradeSagaStore) GetByOrder(_ context.Context, orderID string) (UpgradeSaga, bool, error) {
	saga, ok := s.byOrder[orderID]
	return saga, ok, nil
}

func (s *InMemoryUpgradeSagaStore) GetOpenForTenant(_ context.Context, tenantID string) (UpgradeSaga, bool, error) {
	orderID, ok := s.byTenant[tenantID]
	if !ok {
		return UpgradeSaga{}, false, nil
	}
	saga, ok := s.byOrder[orderID]
	if !ok || saga.IsTerminal() {
		delete(s.byTenant, tenantID)
		return UpgradeSaga{}, false, nil
	}
	return saga, true, nil
}

func (s *InMemoryUpgradeSagaStore) Save(_ context.Context, saga UpgradeSaga) error {
	s.byOrder[saga.OrderID] = saga
	if !saga.IsTerminal() {
		s.byTenant[saga.TenantID] = saga.OrderID
	}
	return nil
}

func (s *InMemoryUpgradeSagaStore) Update(_ context.Context, saga UpgradeSaga) error {
	s.byOrder[saga.OrderID] = saga
	if saga.IsTerminal() {
		delete(s.byTenant, saga.TenantID)
	} else {
		s.byTenant[saga.TenantID] = saga.OrderID
	}
	return nil
}
