package billingprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SubscriptionItemType distinguishes AI plan items from storage add-on items.
type SubscriptionItemType string

const (
	SubscriptionItemTypeAI      SubscriptionItemType = "ai"
	SubscriptionItemTypeStorage SubscriptionItemType = "storage"
)

// SubscriptionItem is a line item within a provider subscription.
type SubscriptionItem struct {
	ID           string
	SubscriptionID string
	PriceID      string
	ItemType     SubscriptionItemType
	Quantity     int64
	CreatedAt    time.Time
}

// StorageAddonGrant records an issued storage add-on entitlement.
type StorageAddonGrant struct {
	ID            uuid.UUID
	TenantID      string
	OrderID       string
	InvoiceID     string
	StorageBytes  int64
	EgressBytes   int64 // proportional to paid share of month
	PeriodStart   time.Time
	PeriodEnd     time.Time
	GrantedAt     time.Time
}

// StorageAddonGrantStore persists storage grant records.
type StorageAddonGrantStore interface {
	GetByOrder(ctx context.Context, orderID string) (StorageAddonGrant, bool, error)
	Save(ctx context.Context, g StorageAddonGrant) error
}

// StorageAddonService manages storage add-on provisioning.
// Storage-only items survive AI item cancellation (criteria A/B).
type StorageAddonService struct {
	provider   Provider
	grantStore StorageAddonGrantStore
}

// NewStorageAddonService returns a StorageAddonService.
func NewStorageAddonService(p Provider, gs StorageAddonGrantStore) *StorageAddonService {
	return &StorageAddonService{provider: p, grantStore: gs}
}

// FulfillStorageAddon issues a storage grant after invoice is paid.
// Criteria C: egressBytes is proportional to the paid share of the month.
func (s *StorageAddonService) FulfillStorageAddon(ctx context.Context, tenantID, orderID, invoiceID string, storageBytes int64, periodStart, periodEnd time.Time) (StorageAddonGrant, error) {
	if existing, found, err := s.grantStore.GetByOrder(ctx, orderID); err != nil {
		return StorageAddonGrant{}, fmt.Errorf("grant lookup: %w", err)
	} else if found {
		return existing, nil
	}

	inv, err := s.provider.GetInvoice(ctx, invoiceID)
	if err != nil {
		return StorageAddonGrant{}, fmt.Errorf("get invoice: %w", err)
	}
	if inv.Status != "paid" {
		return StorageAddonGrant{}, fmt.Errorf("invoice not paid (status: %q)", inv.Status)
	}

	// Criteria C: egress proportional to remaining paid days in month.
	totalDays := periodEnd.Sub(periodStart).Hours() / 24
	remainingDays := time.Until(periodEnd).Hours() / 24
	if remainingDays < 0 {
		remainingDays = 0
	}
	if remainingDays > totalDays {
		remainingDays = totalDays
	}
	var egressBytes int64
	if totalDays > 0 {
		egressBytes = int64(float64(storageBytes) * remainingDays / totalDays)
	}

	g := StorageAddonGrant{
		ID:           uuid.New(),
		TenantID:     tenantID,
		OrderID:      orderID,
		InvoiceID:    invoiceID,
		StorageBytes: storageBytes,
		EgressBytes:  egressBytes,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
		GrantedAt:    time.Now(),
	}
	if err := s.grantStore.Save(ctx, g); err != nil {
		return StorageAddonGrant{}, fmt.Errorf("save grant: %w", err)
	}
	return g, nil
}

// CanCancelAI returns true if the tenant can cancel the AI item without affecting storage.
// Criteria B: cancelling AI does not cancel the whole subscription.
func (s *StorageAddonService) CanCancelAI(items []SubscriptionItem) bool {
	hasStorage := false
	for _, item := range items {
		if item.ItemType == SubscriptionItemTypeStorage {
			hasStorage = true
			break
		}
	}
	return hasStorage // can cancel AI independently when storage exists
}

// InMemoryStorageAddonGrantStore is an in-memory StorageAddonGrantStore for testing.
type InMemoryStorageAddonGrantStore struct {
	byOrder map[string]StorageAddonGrant
}

// NewInMemoryStorageAddonGrantStore returns a ready-to-use store.
func NewInMemoryStorageAddonGrantStore() *InMemoryStorageAddonGrantStore {
	return &InMemoryStorageAddonGrantStore{byOrder: make(map[string]StorageAddonGrant)}
}

func (s *InMemoryStorageAddonGrantStore) GetByOrder(_ context.Context, orderID string) (StorageAddonGrant, bool, error) {
	g, ok := s.byOrder[orderID]
	return g, ok, nil
}

func (s *InMemoryStorageAddonGrantStore) Save(_ context.Context, g StorageAddonGrant) error {
	s.byOrder[g.OrderID] = g
	return nil
}
