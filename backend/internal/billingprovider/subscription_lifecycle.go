package billingprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ─── Financial Subject Lifecycle (Criteria A, D) ────────────────────────────

// FinancialSubjectState is the lifecycle state of a billing profile's financial subject.
type FinancialSubjectState string

const (
	// FinancialSubjectActive: operations permitted.
	FinancialSubjectActive FinancialSubjectState = "active"
	// FinancialSubjectDeleted: account deletion in progress; new purchases blocked.
	FinancialSubjectDeleted FinancialSubjectState = "deleted"
	// FinancialSubjectTransferred: tenant transferred; the billing profile belongs
	// to another owner — deleting the old user must NOT close it (criteria D).
	FinancialSubjectTransferred FinancialSubjectState = "transferred"
)

// FinancialSubject is a billing profile's financial subject record.
type FinancialSubject struct {
	ID              uuid.UUID
	TenantID        string
	OwnerAccountID  string
	State           FinancialSubjectState
	TransferredTo   string // non-empty when transferred
	CreatedAt       time.Time
	StateChangedAt  time.Time
}

// FinancialSubjectStore persists financial subjects.
type FinancialSubjectStore interface {
	Save(ctx context.Context, s FinancialSubject) error
	GetByTenant(ctx context.Context, tenantID string) (FinancialSubject, bool, error)
	UpdateState(ctx context.Context, tenantID string, state FinancialSubjectState, transferredTo string) error
}

// ErrFinancialSubjectBlocked is returned when an operation is blocked by the
// current lifecycle state of the financial subject.
type ErrFinancialSubjectBlocked struct {
	TenantID string
	State    FinancialSubjectState
	Op       string
}

func (e ErrFinancialSubjectBlocked) Error() string {
	return fmt.Sprintf("BILLING_SUBJECT_BLOCKED: op=%s tenantID=%s state=%s", e.Op, e.TenantID, e.State)
}

// SubscriptionLifecycleService enforces the one-subscription-per-billing-profile
// rule, shared month anchor, and financial-subject lifecycle guard.
type SubscriptionLifecycleService struct {
	provider       Provider
	subjectStore   FinancialSubjectStore
	subscriptions  SubscriptionLifecycleStore
}

// NewSubscriptionLifecycleService constructs the service.
func NewSubscriptionLifecycleService(
	p Provider,
	subjectStore FinancialSubjectStore,
	subscriptions SubscriptionLifecycleStore,
) *SubscriptionLifecycleService {
	return &SubscriptionLifecycleService{
		provider:      p,
		subjectStore:  subjectStore,
		subscriptions: subscriptions,
	}
}

// ─── Subscription Profile (Criteria A, B, C) ────────────────────────────────

// SubscriptionProfile is the single recurring subscription allowed per tenant.
type SubscriptionProfile struct {
	ID             uuid.UUID
	TenantID       string
	ProviderSubID  string
	// MonthAnchor is the billing cycle anchor (day of month) shared by all items.
	// Criteria B: all AI/storage items share this anchor.
	MonthAnchor    int
	AIEnabled      bool
	StorageEnabled bool
	State          string // "active", "past_due", "canceled"
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SubscriptionLifecycleStore persists subscription profiles.
type SubscriptionLifecycleStore interface {
	Save(ctx context.Context, p SubscriptionProfile) error
	GetByTenant(ctx context.Context, tenantID string) (SubscriptionProfile, bool, error)
	UpdateState(ctx context.Context, tenantID, state string) error
}

// CheckoutEligibilityResult holds the result of CheckHostedCheckoutEligible.
type CheckoutEligibilityResult struct {
	Eligible bool
	// Reason is non-empty when Eligible=false.
	Reason string
}

// CheckHostedCheckoutEligible returns whether Hosted Checkout may be used.
// Criteria C: Hosted Checkout subscription is only allowed when there is no
// existing unclosed subscription — add-on goes through invoice saga, not a
// second checkout session.
func (s *SubscriptionLifecycleService) CheckHostedCheckoutEligible(ctx context.Context, tenantID string) (CheckoutEligibilityResult, error) {
	if err := s.checkSubjectActive(ctx, tenantID, "checkout"); err != nil {
		return CheckoutEligibilityResult{Eligible: false, Reason: err.Error()}, nil
	}
	_, found, err := s.subscriptions.GetByTenant(ctx, tenantID)
	if err != nil {
		return CheckoutEligibilityResult{}, fmt.Errorf("get subscription: %w", err)
	}
	if found {
		return CheckoutEligibilityResult{
			Eligible: false,
			Reason:   "BILLING_SUBSCRIPTION_EXISTS: use invoice saga for add-ons (criteria C)",
		}, nil
	}
	return CheckoutEligibilityResult{Eligible: true}, nil
}

// RecordSubscription stores the subscription profile after a successful checkout.
// Criteria A: if a profile already exists for the tenant, returns ErrFinancialSubjectBlocked.
// Criteria B: MonthAnchor is derived from periodStart so all items share it.
func (s *SubscriptionLifecycleService) RecordSubscription(ctx context.Context, tenantID, providerSubID string, periodStart time.Time, aiEnabled, storageEnabled bool) (SubscriptionProfile, error) {
	if err := s.checkSubjectActive(ctx, tenantID, "record_subscription"); err != nil {
		return SubscriptionProfile{}, err
	}
	if _, found, err := s.subscriptions.GetByTenant(ctx, tenantID); err != nil {
		return SubscriptionProfile{}, fmt.Errorf("check existing subscription: %w", err)
	} else if found {
		return SubscriptionProfile{}, ErrFinancialSubjectBlocked{TenantID: tenantID, State: FinancialSubjectActive, Op: "record_subscription: already exists"}
	}
	profile := SubscriptionProfile{
		ID:             uuid.New(),
		TenantID:       tenantID,
		ProviderSubID:  providerSubID,
		MonthAnchor:    periodStart.Day(), // criteria B: shared anchor from period start
		AIEnabled:      aiEnabled,
		StorageEnabled: storageEnabled,
		State:          "active",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := s.subscriptions.Save(ctx, profile); err != nil {
		return SubscriptionProfile{}, fmt.Errorf("save subscription profile: %w", err)
	}
	return profile, nil
}

// AddStorageAddon applies a storage add-on to an existing subscription.
// Criteria C: does NOT create a second subscription — uses the existing profile.
func (s *SubscriptionLifecycleService) AddStorageAddon(ctx context.Context, tenantID string) (SubscriptionProfile, error) {
	if err := s.checkSubjectActive(ctx, tenantID, "add_storage_addon"); err != nil {
		return SubscriptionProfile{}, err
	}
	profile, found, err := s.subscriptions.GetByTenant(ctx, tenantID)
	if err != nil {
		return SubscriptionProfile{}, fmt.Errorf("get subscription: %w", err)
	}
	if !found {
		return SubscriptionProfile{}, fmt.Errorf("BILLING_NO_SUBSCRIPTION: no active subscription for tenant %q", tenantID)
	}
	// Criteria C: update existing profile, no new subscription.
	profile.StorageEnabled = true
	profile.UpdatedAt = time.Now()
	if err := s.subscriptions.Save(ctx, profile); err != nil {
		return SubscriptionProfile{}, fmt.Errorf("save updated profile: %w", err)
	}
	return profile, nil
}

// DeleteUserGuard enforces criteria D: deleting a user from a transferred
// tenant must NOT close the billing profile owned by another owner.
// Returns an error if the financial subject is transferred to someone else.
func (s *SubscriptionLifecycleService) DeleteUserGuard(ctx context.Context, tenantID, requestingAccountID string) error {
	subj, found, err := s.subjectStore.GetByTenant(ctx, tenantID)
	if err != nil || !found {
		return err // nothing to protect
	}
	if subj.State == FinancialSubjectTransferred && subj.TransferredTo != requestingAccountID {
		return ErrFinancialSubjectBlocked{
			TenantID: tenantID,
			State:    FinancialSubjectTransferred,
			Op:       "delete_user: transferred tenant billing profile belongs to another owner (criteria D)",
		}
	}
	return nil
}

// checkSubjectActive returns an error if the financial subject is not active.
func (s *SubscriptionLifecycleService) checkSubjectActive(ctx context.Context, tenantID, op string) error {
	subj, found, err := s.subjectStore.GetByTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("financial subject lookup: %w", err)
	}
	if !found {
		return nil // no subject record = allow (first-time setup)
	}
	if subj.State == FinancialSubjectDeleted {
		return ErrFinancialSubjectBlocked{TenantID: tenantID, State: subj.State, Op: op}
	}
	return nil
}

// ─── In-memory stores for testing ────────────────────────────────────────────

// InMemoryFinancialSubjectStore is an in-memory FinancialSubjectStore.
type InMemoryFinancialSubjectStore struct {
	subjects map[string]FinancialSubject
}

// NewInMemoryFinancialSubjectStore returns a ready-to-use store.
func NewInMemoryFinancialSubjectStore() *InMemoryFinancialSubjectStore {
	return &InMemoryFinancialSubjectStore{subjects: make(map[string]FinancialSubject)}
}

func (s *InMemoryFinancialSubjectStore) Save(_ context.Context, subj FinancialSubject) error {
	s.subjects[subj.TenantID] = subj
	return nil
}

func (s *InMemoryFinancialSubjectStore) GetByTenant(_ context.Context, tenantID string) (FinancialSubject, bool, error) {
	subj, ok := s.subjects[tenantID]
	return subj, ok, nil
}

func (s *InMemoryFinancialSubjectStore) UpdateState(_ context.Context, tenantID string, state FinancialSubjectState, transferredTo string) error {
	subj, ok := s.subjects[tenantID]
	if !ok {
		return fmt.Errorf("financial subject not found: %q", tenantID)
	}
	subj.State = state
	subj.TransferredTo = transferredTo
	subj.StateChangedAt = time.Now()
	s.subjects[tenantID] = subj
	return nil
}

// InMemorySubscriptionLifecycleStore is an in-memory SubscriptionLifecycleStore.
type InMemorySubscriptionLifecycleStore struct {
	profiles map[string]SubscriptionProfile
}

// NewInMemorySubscriptionLifecycleStore returns a ready-to-use store.
func NewInMemorySubscriptionLifecycleStore() *InMemorySubscriptionLifecycleStore {
	return &InMemorySubscriptionLifecycleStore{profiles: make(map[string]SubscriptionProfile)}
}

func (s *InMemorySubscriptionLifecycleStore) Save(_ context.Context, p SubscriptionProfile) error {
	s.profiles[p.TenantID] = p
	return nil
}

func (s *InMemorySubscriptionLifecycleStore) GetByTenant(_ context.Context, tenantID string) (SubscriptionProfile, bool, error) {
	p, ok := s.profiles[tenantID]
	return p, ok, nil
}

func (s *InMemorySubscriptionLifecycleStore) UpdateState(_ context.Context, tenantID, state string) error {
	p, ok := s.profiles[tenantID]
	if !ok {
		return fmt.Errorf("subscription profile not found: %q", tenantID)
	}
	p.State = state
	p.UpdatedAt = time.Now()
	s.profiles[tenantID] = p
	return nil
}
