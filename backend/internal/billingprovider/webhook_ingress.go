package billingprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type WebhookInboxState string

const (
	WebhookInboxStatePending     WebhookInboxState = "pending"
	WebhookInboxStateProcessed   WebhookInboxState = "processed"
	WebhookInboxStateQuarantined WebhookInboxState = "quarantined"
)

var ErrWebhookPayloadConflict = errors.New("BILLING_WEBHOOK_HASH_CONFLICT")

type WebhookInboxEntry struct {
	ID               uuid.UUID
	AccountID        string
	ProviderEventID  string
	EventType        string
	RawPayload       []byte
	ReceivedAt       time.Time
	State            WebhookInboxState
	QuarantineReason string
	LeaseExpiry      *time.Time
	ProcessedAt      *time.Time
}

// Record atomically inserts by (account,event), acknowledges the same bytes,
// and rejects a repeated identity carrying a different payload.
// Production implementations must durably commit before returning created=true.
type WebhookInboxStore interface {
	Record(context.Context, WebhookInboxEntry) (created bool, err error)
	Exists(context.Context, string) (bool, error)
	Save(context.Context, WebhookInboxEntry) error
	Update(context.Context, WebhookInboxEntry) error
	ClaimLease(context.Context, uuid.UUID, time.Duration) (bool, error)
	ListPending(context.Context, int) ([]WebhookInboxEntry, error)
}

type WebhookEventParser interface {
	ParseEvent(payload []byte, signature, secret string) (Event, error)
}

type WebhookInboxRecorder interface {
	Record(context.Context, WebhookInboxEntry) (bool, error)
}

type WebhookIngress struct {
	provider      WebhookEventParser
	inboxStore    WebhookInboxRecorder
	webhookSecret string
}

func NewWebhookIngress(p WebhookEventParser, s WebhookInboxRecorder, secret string) *WebhookIngress {
	return &WebhookIngress{provider: p, inboxStore: s, webhookSecret: secret}
}
func (wi *WebhookIngress) IngestRaw(ctx context.Context, account string, payload []byte, signature string) (*WebhookInboxEntry, error) {
	if wi == nil || wi.provider == nil || wi.inboxStore == nil {
		return nil, ErrUnavailable{}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if account == "" || len(payload) == 0 || wi.webhookSecret == "" {
		return nil, errors.New("STRIPE_WEBHOOK_INPUT_INVALID")
	}
	// Snapshot caller-owned bytes before verification; persist these exact bytes.
	raw := bytes.Clone(payload)
	event, err := wi.provider.ParseEvent(raw, signature, wi.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("STRIPE_SIGNATURE_INVALID: %w", err)
	}
	if event.ID == "" || event.Type == "" || (event.AccountID != "" && event.AccountID != account) {
		return nil, errors.New("STRIPE_EVENT_ACCOUNT_OR_IDENTITY_INVALID")
	}
	entry := WebhookInboxEntry{ID: uuid.New(), AccountID: account, ProviderEventID: event.ID, EventType: event.Type, RawPayload: raw, ReceivedAt: time.Now(), State: WebhookInboxStatePending}
	created, err := wi.inboxStore.Record(ctx, entry)
	if err != nil {
		return nil, fmt.Errorf("inbox record: %w", err)
	}
	if !created {
		return nil, nil
	}
	return &entry, nil
}
func (wi *WebhookIngress) QuarantineEntry(ctx context.Context, e WebhookInboxEntry, reason string) error {
	e.State = WebhookInboxStateQuarantined
	e.QuarantineReason = reason
	store, ok := wi.inboxStore.(interface {
		Update(context.Context, WebhookInboxEntry) error
	})
	if !ok {
		return ErrUnavailable{}
	}
	return store.Update(ctx, e)
}

// InMemoryWebhookInboxStore is a synchronized test store, not durable storage.
type InMemoryWebhookInboxStore struct {
	mu        sync.Mutex
	byEventID map[string]uuid.UUID
	byID      map[uuid.UUID]WebhookInboxEntry
	pending   []uuid.UUID
}

func NewInMemoryWebhookInboxStore() *InMemoryWebhookInboxStore {
	return &InMemoryWebhookInboxStore{byEventID: map[string]uuid.UUID{}, byID: map[uuid.UUID]WebhookInboxEntry{}}
}
func webhookKey(e WebhookInboxEntry) string { return e.AccountID + "\x00" + e.ProviderEventID }
func cloneInbox(e WebhookInboxEntry) WebhookInboxEntry {
	e.RawPayload = bytes.Clone(e.RawPayload)
	if e.LeaseExpiry != nil {
		v := *e.LeaseExpiry
		e.LeaseExpiry = &v
	}
	if e.ProcessedAt != nil {
		v := *e.ProcessedAt
		e.ProcessedAt = &v
	}
	return e
}
func (s *InMemoryWebhookInboxStore) insert(e WebhookInboxEntry) {
	s.byEventID[webhookKey(e)] = e.ID
	s.byID[e.ID] = cloneInbox(e)
	if e.State == WebhookInboxStatePending {
		s.pending = append(s.pending, e.ID)
	}
}
func (s *InMemoryWebhookInboxStore) Record(ctx context.Context, e WebhookInboxEntry) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if e.ID == uuid.Nil || e.AccountID == "" || e.ProviderEventID == "" {
		return false, errors.New("inbox identity incomplete")
	}
	if id, ok := s.byEventID[webhookKey(e)]; ok {
		old := s.byID[id]
		if sha256.Sum256(old.RawPayload) != sha256.Sum256(e.RawPayload) || old.EventType != e.EventType {
			return false, ErrWebhookPayloadConflict
		}
		return false, nil
	}
	if _, ok := s.byID[e.ID]; ok {
		return false, errors.New("inbox entry identity conflict")
	}
	s.insert(e)
	return true, nil
}
func (s *InMemoryWebhookInboxStore) Exists(ctx context.Context, event string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	for _, e := range s.byID {
		if e.ProviderEventID == event {
			return true, nil
		}
	}
	return false, nil
}
func (s *InMemoryWebhookInboxStore) Save(ctx context.Context, e WebhookInboxEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.ID == uuid.Nil || e.AccountID == "" || e.ProviderEventID == "" {
		return errors.New("inbox identity incomplete")
	}
	if _, ok := s.byEventID[webhookKey(e)]; ok {
		return fmt.Errorf("duplicate event ID: %s", e.ProviderEventID)
	}
	if _, ok := s.byID[e.ID]; ok {
		return errors.New("inbox entry identity conflict")
	}
	s.insert(e)
	return nil
}
func (s *InMemoryWebhookInboxStore) Update(ctx context.Context, e WebhookInboxEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	old, ok := s.byID[e.ID]
	if !ok {
		return errors.New("inbox entry not found")
	}
	if webhookKey(old) != webhookKey(e) || old.EventType != e.EventType || !bytes.Equal(old.RawPayload, e.RawPayload) {
		return ErrWebhookPayloadConflict
	}
	s.byID[e.ID] = cloneInbox(e)
	return nil
}
func (s *InMemoryWebhookInboxStore) ClaimLease(ctx context.Context, id uuid.UUID, d time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d <= 0 {
		return false, errors.New("inbox lease duration must be positive")
	}
	e, ok := s.byID[id]
	if !ok {
		return false, errors.New("inbox entry not found")
	}
	now := time.Now()
	if e.State != WebhookInboxStatePending || (e.LeaseExpiry != nil && now.Before(*e.LeaseExpiry)) {
		return false, nil
	}
	deadline := now.Add(d)
	e.LeaseExpiry = &deadline
	s.byID[id] = e
	return true, nil
}
func (s *InMemoryWebhookInboxStore) ListPending(ctx context.Context, limit int) ([]WebhookInboxEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 0 {
		return nil, errors.New("inbox limit must not be negative")
	}
	out := []WebhookInboxEntry{}
	if limit == 0 {
		return out, nil
	}
	now := time.Now()
	for _, id := range s.pending {
		e, ok := s.byID[id]
		if !ok || e.State != WebhookInboxStatePending || (e.LeaseExpiry != nil && now.Before(*e.LeaseExpiry)) {
			continue
		}
		out = append(out, cloneInbox(e))
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
func (s *InMemoryWebhookInboxStore) CountInbox() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byID)
}
