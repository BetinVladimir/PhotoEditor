package billingprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ReconciliationRecord tracks a reconciliation run for audit purposes.
type ReconciliationRecord struct {
	ID           uuid.UUID
	TenantID     string
	RestoredAt   time.Time
	// GrantsRestored counts grants re-created from provider invoices.
	GrantsRestored int
	// TombstonesPreserved counts erasure tombstones that prevented resurrection.
	TombstonesPreserved int
}

// ErasureTombstone marks a deleted account so it cannot be resurrected.
// Criteria C: deleted account stays deleted.
type ErasureTombstone struct {
	TenantID  string
	DeletedAt time.Time
}

// TombstoneStore persists erasure tombstones.
type TombstoneStore interface {
	Exists(ctx context.Context, tenantID string) (bool, error)
}

// ReconciliationService restores missing grants after a DB restore or lost webhook.
// Criteria A: lost webhook → reconcile from provider paid invoices.
// Criteria B: erasure tombstones are applied before granting.
// Criteria C: deleted account does not resurrect.
type ReconciliationService struct {
	provider    Provider
	grantStore  GrantStore
	tombstones  TombstoneStore
}

// NewReconciliationService returns a ReconciliationService.
func NewReconciliationService(p Provider, gs GrantStore, ts TombstoneStore) *ReconciliationService {
	return &ReconciliationService{provider: p, grantStore: gs, tombstones: ts}
}

// ReconcileInvoice restores a grant for a paid invoice if one is missing.
// Criteria B: tombstone for the tenant → skip (account deleted, criteria C).
func (rs *ReconciliationService) ReconcileInvoice(ctx context.Context, tenantID, orderID, invoiceID, lineItemID, planKey string, periodStart, periodEnd time.Time) (ReconciliationRecord, error) {
	rec := ReconciliationRecord{
		ID:         uuid.New(),
		TenantID:   tenantID,
		RestoredAt: time.Now(),
	}

	// Criteria B+C: check tombstone before granting.
	deleted, err := rs.tombstones.Exists(ctx, tenantID)
	if err != nil {
		return rec, fmt.Errorf("tombstone check: %w", err)
	}
	if deleted {
		rec.TombstonesPreserved++
		return rec, nil // skip: deleted account must not resurrect
	}

	// Check if grant already exists.
	if _, found, err := rs.grantStore.GetByInvoice(ctx, invoiceID); err != nil {
		return rec, fmt.Errorf("grant lookup: %w", err)
	} else if found {
		return rec, nil // already present — no restore needed
	}

	// Read invoice authoritatively.
	inv, err := rs.provider.GetInvoice(ctx, invoiceID)
	if err != nil {
		return rec, fmt.Errorf("get invoice: %w", err)
	}
	if inv.Status != "paid" {
		return rec, fmt.Errorf("invoice %q not paid (status: %q) — not restoring", invoiceID, inv.Status)
	}

	// Criteria A: restore missing grant.
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
	if err := rs.grantStore.Save(ctx, g); err != nil {
		return rec, fmt.Errorf("save restored grant: %w", err)
	}
	rec.GrantsRestored++
	return rec, nil
}

// InMemoryTombstoneStore is an in-memory TombstoneStore for testing.
type InMemoryTombstoneStore struct {
	deleted map[string]bool
}

// NewInMemoryTombstoneStore returns a ready-to-use store.
func NewInMemoryTombstoneStore() *InMemoryTombstoneStore {
	return &InMemoryTombstoneStore{deleted: make(map[string]bool)}
}

// AddTombstone marks a tenant as deleted.
func (s *InMemoryTombstoneStore) AddTombstone(tenantID string) {
	s.deleted[tenantID] = true
}

func (s *InMemoryTombstoneStore) Exists(_ context.Context, tenantID string) (bool, error) {
	return s.deleted[tenantID], nil
}
