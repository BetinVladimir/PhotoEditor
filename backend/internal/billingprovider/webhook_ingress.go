package billingprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// WebhookInboxState tracks processing state for a received event.
type WebhookInboxState string

const (
	WebhookInboxStatePending     WebhookInboxState = "pending"
	WebhookInboxStateProcessed   WebhookInboxState = "processed"
	WebhookInboxStateQuarantined WebhookInboxState = "quarantined"
)

// WebhookInboxEntry is a persisted record of a received provider event.
// Written before returning 2xx so no event is lost.
type WebhookInboxEntry struct {
	ID             uuid.UUID
	AccountID      string // Stripe account that sent the event
	ProviderEventID string // provider-side dedup key
	EventType      string
	RawPayload     []byte
	ReceivedAt     time.Time
	State          WebhookInboxState
	QuarantineReason string // set when state == quarantined
	LeaseExpiry    *time.Time // set when a worker holds a retry lease
	ProcessedAt    *time.Time
}

// WebhookInboxStore persists inbox entries.
type WebhookInboxStore interface {
	// Exists returns true if an entry with this providerEventID already exists.
	Exists(ctx context.Context, providerEventID string) (bool, error)
	// Save writes a new entry; must fail if providerEventID already exists.
	Save(ctx context.Context, entry WebhookInboxEntry) error
	// Update persists state changes (quarantine, processed, lease).
	Update(ctx context.Context, entry WebhookInboxEntry) error
	// ClaimLease acquires a processing lease; returns false if already leased.
	ClaimLease(ctx context.Context, entryID uuid.UUID, leaseDuration time.Duration) (bool, error)
	// ListPending returns up to limit entries eligible for processing.
	ListPending(ctx context.Context, limit int) ([]WebhookInboxEntry, error)
}

// WebhookIngress verifies and stores incoming provider webhook events.
// It writes to the inbox before returning 2xx, enforcing at-least-once delivery.
// Criteria A: invalid signature is rejected before any inbox write.
// Criteria B: duplicate providerEventID is detected and skipped silently.
// Criteria C: unexpected customer/order/mode/currency/price → quarantine.
// Criteria D: exact event list and payload paths per OPEN-13 (not yet known).
type WebhookIngress struct {
	provider    Provider
	inboxStore  WebhookInboxStore
	webhookSecret string
}

// NewWebhookIngress returns a WebhookIngress for the given provider and inbox store.
// webhookSecret is the raw secret value read from the *_REF file at runtime.
func NewWebhookIngress(p Provider, s WebhookInboxStore, webhookSecret string) *WebhookIngress {
	return &WebhookIngress{provider: p, inboxStore: s, webhookSecret: webhookSecret}
}

// IngestRaw receives a raw webhook payload, verifies its signature, and writes
// it to the inbox. Returns the inbox entry on success.
// Criteria A: bad signature → error, inbox not written.
// Criteria B: known providerEventID → returns nil error (duplicate acknowledged).
func (wi *WebhookIngress) IngestRaw(ctx context.Context, accountID string, payload []byte, signature string) (*WebhookInboxEntry, error) {
	// Criteria A: verify signature before any side effects.
	event, err := wi.provider.ParseEvent(payload, signature, wi.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("STRIPE_SIGNATURE_INVALID: %w", err)
	}

	// Criteria B: deduplicate by providerEventID.
	if exists, err := wi.inboxStore.Exists(ctx, event.ID); err != nil {
		return nil, fmt.Errorf("inbox dedup check: %w", err)
	} else if exists {
		return nil, nil // duplicate — acknowledged silently, no double-write
	}

	entry := WebhookInboxEntry{
		ID:              uuid.New(),
		AccountID:       accountID,
		ProviderEventID: event.ID,
		EventType:       event.Type,
		RawPayload:      payload,
		ReceivedAt:      time.Now(),
		State:           WebhookInboxStatePending,
	}
	if err := wi.inboxStore.Save(ctx, entry); err != nil {
		return nil, fmt.Errorf("inbox save: %w", err)
	}
	return &entry, nil
}

// QuarantineEntry moves an entry to quarantined state with a reason.
// Criteria C: called when processing detects unexpected customer/order/mode/currency/price.
func (wi *WebhookIngress) QuarantineEntry(ctx context.Context, entry WebhookInboxEntry, reason string) error {
	entry.State = WebhookInboxStateQuarantined
	entry.QuarantineReason = reason
	return wi.inboxStore.Update(ctx, entry)
}

// InMemoryWebhookInboxStore is an in-memory WebhookInboxStore for testing.
type InMemoryWebhookInboxStore struct {
	byEventID map[string]bool
	byID      map[uuid.UUID]WebhookInboxEntry
	pending   []uuid.UUID
}

// NewInMemoryWebhookInboxStore returns a ready-to-use in-memory inbox store.
func NewInMemoryWebhookInboxStore() *InMemoryWebhookInboxStore {
	return &InMemoryWebhookInboxStore{
		byEventID: make(map[string]bool),
		byID:      make(map[uuid.UUID]WebhookInboxEntry),
	}
}

func (s *InMemoryWebhookInboxStore) Exists(_ context.Context, providerEventID string) (bool, error) {
	return s.byEventID[providerEventID], nil
}

func (s *InMemoryWebhookInboxStore) Save(_ context.Context, entry WebhookInboxEntry) error {
	if s.byEventID[entry.ProviderEventID] {
		return fmt.Errorf("duplicate event ID: %s", entry.ProviderEventID)
	}
	s.byEventID[entry.ProviderEventID] = true
	s.byID[entry.ID] = entry
	if entry.State == WebhookInboxStatePending {
		s.pending = append(s.pending, entry.ID)
	}
	return nil
}

func (s *InMemoryWebhookInboxStore) Update(_ context.Context, entry WebhookInboxEntry) error {
	s.byID[entry.ID] = entry
	s.byEventID[entry.ProviderEventID] = true
	return nil
}

func (s *InMemoryWebhookInboxStore) ClaimLease(_ context.Context, entryID uuid.UUID, dur time.Duration) (bool, error) {
	e, ok := s.byID[entryID]
	if !ok {
		return false, fmt.Errorf("entry not found: %s", entryID)
	}
	if e.State != WebhookInboxStatePending {
		return false, nil
	}
	now := time.Now()
	exp := now.Add(dur)
	if e.LeaseExpiry != nil && now.Before(*e.LeaseExpiry) {
		return false, nil // already leased
	}
	e.LeaseExpiry = &exp
	s.byID[entryID] = e
	return true, nil
}

func (s *InMemoryWebhookInboxStore) ListPending(_ context.Context, limit int) ([]WebhookInboxEntry, error) {
	var out []WebhookInboxEntry
	now := time.Now()
	for _, id := range s.pending {
		e, ok := s.byID[id]
		if !ok {
			continue
		}
		if e.State != WebhookInboxStatePending {
			continue
		}
		if e.LeaseExpiry != nil && now.Before(*e.LeaseExpiry) {
			continue // currently leased
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// CountInbox returns the total number of entries (for test assertions).
func (s *InMemoryWebhookInboxStore) CountInbox() int {
	return len(s.byID)
}
