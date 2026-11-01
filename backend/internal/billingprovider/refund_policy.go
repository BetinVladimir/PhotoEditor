package billingprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RefundAllocation maps a refund to specific order line items with quantities.
// Criteria B: partial refund without unambiguous quantity requires explicit
// operator distribution (auto-guessing is prohibited).
type RefundAllocation struct {
	LineItemID string
	Quantity   int64
	Amount     int64 // minor units
}

// RefundRecord persists a refund with its allocations and entitlement revocation.
type RefundRecord struct {
	ID             uuid.UUID
	TenantID       string
	OrderID        string
	ProviderRefundID string
	Allocations    []RefundAllocation
	// ConsumedRemains is always true: consumed entitlement is not revoked.
	ConsumedRemains bool
	// UnusedRevoked is true when the unused remainder was revoked.
	UnusedRevoked bool
	// IsDisputed is set when the refund originated from a payment dispute.
	IsDisputed bool
	CreatedAt  time.Time
}

// RefundRecordStore persists refund records.
type RefundRecordStore interface {
	GetByRefundID(ctx context.Context, providerRefundID string) (RefundRecord, bool, error)
	Save(ctx context.Context, r RefundRecord) error
}

// DisputeState tracks a payment dispute.
type DisputeState string

const (
	DisputeStateOpen   DisputeState = "open"
	DisputeStateClosed DisputeState = "closed"
)

// DisputeRecord is a persisted payment dispute.
type DisputeRecord struct {
	ID             uuid.UUID
	TenantID       string
	ProviderID     string
	State          DisputeState
	// BlocksRiskyAdmissions: when true, new risky commercial operations are blocked.
	// Criteria C: deletion rights and data access are not blocked.
	BlocksRiskyAdmissions bool
	CreatedAt time.Time
}

// DisputeStore persists dispute records.
type DisputeStore interface {
	GetByProvider(ctx context.Context, providerID string) (DisputeRecord, bool, error)
	GetOpenForTenant(ctx context.Context, tenantID string) (DisputeRecord, bool, error)
	Save(ctx context.Context, d DisputeRecord) error
	Update(ctx context.Context, d DisputeRecord) error
}

// RefundPolicyService manages refunds and disputes.
type RefundPolicyService struct {
	provider      Provider
	refundStore   RefundRecordStore
	disputeStore  DisputeStore
}

// NewRefundPolicyService returns a RefundPolicyService.
func NewRefundPolicyService(p Provider, rs RefundRecordStore, ds DisputeStore) *RefundPolicyService {
	return &RefundPolicyService{provider: p, refundStore: rs, disputeStore: ds}
}

// RecordRefund stores a refund with its explicit allocations.
// Criteria A: consumed entitlement is preserved; unused remainder is revoked.
// Criteria B: allocations must be provided explicitly — no auto-distribution.
// Criteria D: invoice and financial logs are NOT deleted.
func (rps *RefundPolicyService) RecordRefund(ctx context.Context, tenantID, orderID, providerRefundID string, allocations []RefundAllocation, unusedBytes int64) (RefundRecord, error) {
	if existing, found, err := rps.refundStore.GetByRefundID(ctx, providerRefundID); err != nil {
		return RefundRecord{}, fmt.Errorf("refund lookup: %w", err)
	} else if found {
		return existing, nil
	}

	// Criteria B: reject if no explicit allocations for a partial refund.
	if len(allocations) == 0 {
		return RefundRecord{}, fmt.Errorf("REFUND_ALLOCATION_REQUIRED: explicit allocation required for refund")
	}

	// Verify refund exists in provider.
	if _, err := rps.provider.GetRefund(ctx, providerRefundID); err != nil {
		return RefundRecord{}, fmt.Errorf("get refund: %w", err)
	}

	r := RefundRecord{
		ID:               uuid.New(),
		TenantID:         tenantID,
		OrderID:          orderID,
		ProviderRefundID: providerRefundID,
		Allocations:      allocations,
		ConsumedRemains:  true,      // criteria A: consumed stays in history
		UnusedRevoked:    unusedBytes > 0,
		CreatedAt:        time.Now(),
	}
	if err := rps.refundStore.Save(ctx, r); err != nil {
		return RefundRecord{}, fmt.Errorf("save refund: %w", err)
	}
	return r, nil
}

// OpenDispute records a new payment dispute.
// Criteria C: dispute blocks new risky admissions but not deletion/data rights.
func (rps *RefundPolicyService) OpenDispute(ctx context.Context, tenantID, providerDisputeID string) (DisputeRecord, error) {
	if existing, found, err := rps.disputeStore.GetByProvider(ctx, providerDisputeID); err != nil {
		return DisputeRecord{}, fmt.Errorf("dispute lookup: %w", err)
	} else if found {
		return existing, nil
	}

	d := DisputeRecord{
		ID:                    uuid.New(),
		TenantID:              tenantID,
		ProviderID:            providerDisputeID,
		State:                 DisputeStateOpen,
		BlocksRiskyAdmissions: true, // criteria C: commercial block only
		CreatedAt:             time.Now(),
	}
	if err := rps.disputeStore.Save(ctx, d); err != nil {
		return DisputeRecord{}, fmt.Errorf("save dispute: %w", err)
	}
	return d, nil
}

// CloseDispute marks a dispute resolved.
func (rps *RefundPolicyService) CloseDispute(ctx context.Context, providerDisputeID string) (DisputeRecord, error) {
	d, found, err := rps.disputeStore.GetByProvider(ctx, providerDisputeID)
	if err != nil || !found {
		return DisputeRecord{}, fmt.Errorf("dispute not found %q: %w", providerDisputeID, err)
	}
	d.State = DisputeStateClosed
	d.BlocksRiskyAdmissions = false
	if err := rps.disputeStore.Update(ctx, d); err != nil {
		return DisputeRecord{}, fmt.Errorf("update dispute: %w", err)
	}
	return d, nil
}

// IsRiskyAdmissionBlocked returns true if the tenant has an open dispute.
func (rps *RefundPolicyService) IsRiskyAdmissionBlocked(ctx context.Context, tenantID string) (bool, error) {
	d, found, err := rps.disputeStore.GetOpenForTenant(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return found && d.BlocksRiskyAdmissions, nil
}

// InMemory implementations for testing.

type InMemoryRefundRecordStore struct{ m map[string]RefundRecord }

func NewInMemoryRefundRecordStore() *InMemoryRefundRecordStore {
	return &InMemoryRefundRecordStore{m: make(map[string]RefundRecord)}
}
func (s *InMemoryRefundRecordStore) GetByRefundID(_ context.Context, id string) (RefundRecord, bool, error) {
	r, ok := s.m[id]
	return r, ok, nil
}
func (s *InMemoryRefundRecordStore) Save(_ context.Context, r RefundRecord) error {
	s.m[r.ProviderRefundID] = r
	return nil
}

type InMemoryDisputeStore struct {
	byProvider map[string]DisputeRecord
	byTenant   map[string]string // tenantID → providerID
}

func NewInMemoryDisputeStore() *InMemoryDisputeStore {
	return &InMemoryDisputeStore{
		byProvider: make(map[string]DisputeRecord),
		byTenant:   make(map[string]string),
	}
}
func (s *InMemoryDisputeStore) GetByProvider(_ context.Context, id string) (DisputeRecord, bool, error) {
	d, ok := s.byProvider[id]
	return d, ok, nil
}
func (s *InMemoryDisputeStore) GetOpenForTenant(_ context.Context, tenantID string) (DisputeRecord, bool, error) {
	id, ok := s.byTenant[tenantID]
	if !ok {
		return DisputeRecord{}, false, nil
	}
	d, ok := s.byProvider[id]
	if !ok || d.State != DisputeStateOpen {
		delete(s.byTenant, tenantID)
		return DisputeRecord{}, false, nil
	}
	return d, true, nil
}
func (s *InMemoryDisputeStore) Save(_ context.Context, d DisputeRecord) error {
	s.byProvider[d.ProviderID] = d
	if d.State == DisputeStateOpen {
		s.byTenant[d.TenantID] = d.ProviderID
	}
	return nil
}
func (s *InMemoryDisputeStore) Update(_ context.Context, d DisputeRecord) error {
	s.byProvider[d.ProviderID] = d
	if d.State != DisputeStateOpen {
		delete(s.byTenant, d.TenantID)
	}
	return nil
}

// Refundable returns time.Time zero; used to mark a grant entry as refund-eligible.
func RefundableAt() time.Time { return time.Time{} }
