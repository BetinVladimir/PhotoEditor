package billingprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ─── Customer Portal (Criterion A) ─────────────────────────────────────────

// PortalSessionService creates customer portal sessions with plan changes
// disabled. Criterion A: plan change outside our saga is impossible because
// only the PortalConfigID that has subscription_update disabled is passed.
type PortalSessionService struct {
	provider        Provider
	portalConfigID  string // Stripe portal config with subscription_update disabled
}

// NewPortalSessionService constructs a PortalSessionService.
// portalConfigID must refer to a Stripe portal configuration that has the
// subscription_update feature disabled; callers are responsible for setting
// this up in the Stripe test dashboard (see STR-14/15).
func NewPortalSessionService(p Provider, portalConfigID string) *PortalSessionService {
	return &PortalSessionService{provider: p, portalConfigID: portalConfigID}
}

// PortalSessionResult holds the result of CreateSession.
type PortalSessionResult struct {
	Session        PortalSession
	PlanChangeMode string // always "disabled" (criteria A)
}

// CreateSession creates a portal session. The configurationID is always set
// to the portal config that disables plan changes (Criterion A).
func (s *PortalSessionService) CreateSession(ctx context.Context, tenantID, customerID, returnURL, idempotencyKey string) (PortalSessionResult, error) {
	params := CreatePortalSessionParams{
		CustomerID:      customerID,
		ReturnURL:       returnURL,
		ConfigurationID: s.portalConfigID,
	}
	hash := fmt.Sprintf("portal:%s:%s", tenantID, idempotencyKey)
	sess, err := s.provider.CreatePortalSession(ctx, idempotencyKey, hash, params)
	if err != nil {
		return PortalSessionResult{}, fmt.Errorf("create portal session: %w", err)
	}
	return PortalSessionResult{
		Session:        sess,
		PlanChangeMode: "disabled",
	}, nil
}

// ─── Dunning Policy (Criterion B) ──────────────────────────────────────────

// DunningState is the lifecycle of a failed renewal.
type DunningState string

const (
	DunningStateOpen      DunningState = "open"
	DunningStateRetrying  DunningState = "retrying"
	DunningStateRecovered DunningState = "recovered"
	DunningStateFailed    DunningState = "failed"
)

// DunningRecord tracks a failed recurring invoice payment.
type DunningRecord struct {
	ID             uuid.UUID
	TenantID       string
	SubscriptionID string
	InvoiceID      string
	State          DunningState
	FailedAt       time.Time
	ResolvedAt     *time.Time
	// GrantBlocked is true while the invoice is unpaid (Criterion B).
	GrantBlocked bool
}

// DunningStore persists dunning records.
type DunningStore interface {
	Save(ctx context.Context, r DunningRecord) error
	GetByInvoice(ctx context.Context, invoiceID string) (DunningRecord, bool, error)
	UpdateState(ctx context.Context, invoiceID string, state DunningState, resolvedAt *time.Time) error
}

// DunningPolicy manages renewal-failure lifecycle.
// Criterion B: no grant is issued for any invoice that is not "paid".
type DunningPolicy struct {
	provider Provider
	store    DunningStore
}

// NewDunningPolicy constructs a DunningPolicy.
func NewDunningPolicy(p Provider, store DunningStore) *DunningPolicy {
	return &DunningPolicy{provider: p, store: store}
}

// RecordFailure opens a dunning record when a renewal invoice payment fails.
// Criterion B: GrantBlocked=true until RecoverInvoice clears it.
func (d *DunningPolicy) RecordFailure(ctx context.Context, tenantID, subscriptionID, invoiceID string) (DunningRecord, error) {
	if _, exists, _ := d.store.GetByInvoice(ctx, invoiceID); exists {
		r, _, err := d.store.GetByInvoice(ctx, invoiceID)
		return r, err
	}
	r := DunningRecord{
		ID:             uuid.New(),
		TenantID:       tenantID,
		SubscriptionID: subscriptionID,
		InvoiceID:      invoiceID,
		State:          DunningStateOpen,
		FailedAt:       time.Now(),
		GrantBlocked:   true,
	}
	if err := d.store.Save(ctx, r); err != nil {
		return DunningRecord{}, fmt.Errorf("save dunning record: %w", err)
	}
	return r, nil
}

// AttemptRecovery transitions an open record to retrying.
func (d *DunningPolicy) AttemptRecovery(ctx context.Context, invoiceID string) (DunningRecord, error) {
	r, found, err := d.store.GetByInvoice(ctx, invoiceID)
	if err != nil || !found {
		return DunningRecord{}, fmt.Errorf("dunning record not found for invoice %q", invoiceID)
	}
	if r.State != DunningStateOpen && r.State != DunningStateRetrying {
		return r, fmt.Errorf("cannot retry from state %q", r.State)
	}
	if err := d.store.UpdateState(ctx, invoiceID, DunningStateRetrying, nil); err != nil {
		return DunningRecord{}, fmt.Errorf("update dunning state: %w", err)
	}
	r.State = DunningStateRetrying
	return r, nil
}

// RecoverInvoice marks a dunning record as recovered once the invoice is paid.
// Criterion B: only a paid invoice clears GrantBlocked.
func (d *DunningPolicy) RecoverInvoice(ctx context.Context, invoiceID string) (DunningRecord, error) {
	inv, err := d.provider.GetInvoice(ctx, invoiceID)
	if err != nil {
		return DunningRecord{}, fmt.Errorf("get invoice: %w", err)
	}
	if inv.Status != "paid" {
		return DunningRecord{}, fmt.Errorf("BILLING_DUNNING_INVOICE_NOT_PAID: invoice %q status=%q", invoiceID, inv.Status)
	}
	r, found, err := d.store.GetByInvoice(ctx, invoiceID)
	if err != nil || !found {
		return DunningRecord{}, fmt.Errorf("dunning record not found for invoice %q", invoiceID)
	}
	now := time.Now()
	if err := d.store.UpdateState(ctx, invoiceID, DunningStateRecovered, &now); err != nil {
		return DunningRecord{}, fmt.Errorf("update dunning state: %w", err)
	}
	r.State = DunningStateRecovered
	r.ResolvedAt = &now
	r.GrantBlocked = false
	return r, nil
}

// IsGrantBlocked returns true if the invoice has an open or retrying dunning
// record (Criterion B: no grant on unpaid invoice).
func (d *DunningPolicy) IsGrantBlocked(ctx context.Context, invoiceID string) (bool, error) {
	r, found, err := d.store.GetByInvoice(ctx, invoiceID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	return r.GrantBlocked && (r.State == DunningStateOpen || r.State == DunningStateRetrying), nil
}

// InMemoryDunningStore is an in-memory DunningStore for testing.
type InMemoryDunningStore struct {
	records map[string]DunningRecord // keyed by invoiceID
}

// NewInMemoryDunningStore returns a ready-to-use store.
func NewInMemoryDunningStore() *InMemoryDunningStore {
	return &InMemoryDunningStore{records: make(map[string]DunningRecord)}
}

func (s *InMemoryDunningStore) Save(_ context.Context, r DunningRecord) error {
	s.records[r.InvoiceID] = r
	return nil
}

func (s *InMemoryDunningStore) GetByInvoice(_ context.Context, invoiceID string) (DunningRecord, bool, error) {
	r, ok := s.records[invoiceID]
	return r, ok, nil
}

func (s *InMemoryDunningStore) UpdateState(_ context.Context, invoiceID string, state DunningState, resolvedAt *time.Time) error {
	r, ok := s.records[invoiceID]
	if !ok {
		return fmt.Errorf("dunning record not found: %q", invoiceID)
	}
	r.State = state
	if resolvedAt != nil {
		r.ResolvedAt = resolvedAt
		r.GrantBlocked = false
	}
	s.records[invoiceID] = r
	return nil
}

// ─── CI Notification Guard (Criterion C) ───────────────────────────────────

// ErrProductionEmailBlocked is returned when a non-test address is routed
// in CI/test mode (Criterion C: CI must never send production emails).
type ErrProductionEmailBlocked struct {
	Address string
}

func (e ErrProductionEmailBlocked) Error() string {
	return fmt.Sprintf("BILLING_CI_EMAIL_BLOCKED: production address %q rejected in test mode", e.Address)
}

// NotificationGuard checks email addresses before any send.
// Criterion C: when TestMode=true, only addresses matching the allowlist are
// permitted; any other address returns ErrProductionEmailBlocked.
type NotificationGuard struct {
	testMode         bool
	allowedDomains   []string // e.g. ["example.com", "test.invalid"]
	allowedAddresses []string // literal allowed addresses (optional)
}

// NewNotificationGuard returns a guard.
// testMode should be true in CI and unit tests (never false in those contexts).
// allowedDomains and allowedAddresses list the only recipients permitted in test mode.
func NewNotificationGuard(testMode bool, allowedDomains, allowedAddresses []string) *NotificationGuard {
	return &NotificationGuard{
		testMode:         testMode,
		allowedDomains:   allowedDomains,
		allowedAddresses: allowedAddresses,
	}
}

// CheckAddress returns nil if the address is permitted.
// In non-test mode all addresses pass. In test mode only allowlisted
// domains and explicit addresses are permitted (Criterion C).
func (g *NotificationGuard) CheckAddress(address string) error {
	if !g.testMode {
		return nil
	}
	for _, a := range g.allowedAddresses {
		if strings.EqualFold(a, address) {
			return nil
		}
	}
	at := strings.LastIndex(address, "@")
	if at >= 0 {
		domain := strings.ToLower(address[at+1:])
		for _, d := range g.allowedDomains {
			if strings.ToLower(d) == domain {
				return nil
			}
		}
	}
	return ErrProductionEmailBlocked{Address: address}
}

// IsTestMode returns whether the guard is in test/CI mode.
func (g *NotificationGuard) IsTestMode() bool { return g.testMode }
