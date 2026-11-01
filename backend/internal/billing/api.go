// Package billing — API service interfaces and DTO types for billing HTTP endpoints.
package billing

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

// Money represents a monetary amount with tax breakdown.
type Money struct {
	Currency  string      `json:"currency"`
	NetMinor  string      `json:"net_minor"`
	TaxMinor  string      `json:"tax_minor"`
	GrossMinor string     `json:"gross_minor"`
	TaxMode   string      `json:"tax_mode"`
	TaxLines  []TaxLine   `json:"tax_lines"`
}

// TaxLine is a single tax component within Money.
type TaxLine struct {
	Code        string `json:"code"`
	RateDecimal string `json:"rate_decimal"`
	AmountMinor string `json:"amount_minor"`
}

// PlanLimit describes a single resource limit on a plan.
type PlanLimit struct {
	ResourceCode string `json:"resource_code"`
	Quantity     string `json:"quantity"`
	Unit         string `json:"unit"`
}

// PlanEntry is one published plan in the billing catalog.
type PlanEntry struct {
	ID                uuid.UUID   `json:"id"`
	VersionID         uuid.UUID   `json:"version_id"`
	Code              string      `json:"code"`
	Label             string      `json:"label"`
	Price             Money       `json:"price"`
	Limits            []PlanLimit `json:"limits"`
	CanPurchase       bool        `json:"can_purchase"`
	UnavailableReason *string     `json:"unavailable_reason"`
}

// Catalog is the public billing catalog response.
type Catalog struct {
	CatalogRevision string      `json:"catalog_revision"`
	Scope           string      `json:"scope"`
	Currency        string      `json:"currency"`
	Plans           []PlanEntry `json:"plans"`
}

// ResourceBalance is current resource usage for one resource.
type ResourceBalance struct {
	ResourceCode  string     `json:"resource_code"`
	Unit          string     `json:"unit"`
	Granted       string     `json:"granted"`
	Consumed      string     `json:"consumed"`
	Allocated     string     `json:"allocated"`
	Available     string     `json:"available"`
	Overrun       string     `json:"overrun"`
	Blocked       bool       `json:"blocked"`
	BlockedReason *string    `json:"blocked_reason"`
	NextExpiryAt  *time.Time `json:"next_expiry_at"`
	AsOf          time.Time  `json:"as_of"`
}

// StorageCategories breaks down storage by object category.
type StorageCategories struct {
	Original string `json:"original"`
	Result   string `json:"result"`
	Preview  string `json:"preview"`
	Archive  string `json:"archive"`
}

// StorageBalance is the current storage usage summary.
type StorageBalance struct {
	Included       string            `json:"included"`
	Additional     string            `json:"additional"`
	Total          string            `json:"total"`
	Committed      string            `json:"committed"`
	Reserved       string            `json:"reserved"`
	Free           string            `json:"free"`
	OverCapacity   bool              `json:"over_capacity"`
	Categories     StorageCategories `json:"categories"`
	PolicyRevision string            `json:"policy_revision"`
	AsOf           time.Time         `json:"as_of"`
}

// SubscriptionItem is one line of a subscription.
type SubscriptionItem struct {
	ID           uuid.UUID `json:"id"`
	ResourceCode string    `json:"resource_code"`
	Quantity     string    `json:"quantity"`
	Price        Money     `json:"price"`
}

// PendingChange is a scheduled subscription change.
type PendingChange struct {
	ID          uuid.UUID `json:"id"`
	Type        string    `json:"type"`
	EffectiveAt time.Time `json:"effective_at"`
}

// SummaryActions describes what the owner can do.
type SummaryActions struct {
	CanUpgrade       bool `json:"can_upgrade"`
	CanDowngrade     bool `json:"can_downgrade"`
	CanCancel        bool `json:"can_cancel"`
	CanReactivate    bool `json:"can_reactivate"`
	CanPurchaseAddons bool `json:"can_purchase_addons"`
}

// SummaryPlan is the plan field within BillingSummary.
type SummaryPlan struct {
	Code    string `json:"code"`
	Version string `json:"version"`
	NameKey string `json:"name_key"`
	Custom  bool   `json:"custom"`
}

// SummaryPeriod is the billing period within BillingSummary.
type SummaryPeriod struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

// Summary is the full billing status for a tenant.
type Summary struct {
	TenantID            uuid.UUID          `json:"tenant_id"`
	Revision            string             `json:"revision"`
	EntitlementRevision string             `json:"entitlement_revision"`
	Plan                SummaryPlan        `json:"plan"`
	EntitlementState    string             `json:"entitlement_state"`
	BillingMode         string             `json:"billing_mode"`
	Period              *SummaryPeriod     `json:"period"`
	Balances            []ResourceBalance  `json:"balances"`
	Storage             StorageBalance     `json:"storage"`
	RecurringTotal      *Money             `json:"recurring_total"`
	Items               []SubscriptionItem `json:"items"`
	PendingChange       *PendingChange     `json:"pending_change"`
	Actions             SummaryActions     `json:"actions"`
}

// UsageSummaryResponse is the response for GET /billing/usage.
type UsageSummaryResponse struct {
	Balances        []ResourceBalance `json:"balances"`
	Storage         StorageBalance    `json:"storage"`
	AsOf            time.Time         `json:"as_of"`
	SummaryRevision string            `json:"summary_revision"`
}

// BucketEntry is one credit bucket in a paginated list.
type BucketEntry struct {
	ID           uuid.UUID  `json:"id"`
	Resource     string     `json:"resource"`
	Remaining    string     `json:"remaining"`
	Allocated    string     `json:"allocated"`
	Consumed     string     `json:"consumed"`
	ExpiresAt    *time.Time `json:"expires_at"`
	ExpiryPolicy string     `json:"expiry_policy"`
	SourceLabel  string     `json:"source_label"`
}

// UsagePage is a paginated page of credit buckets.
type UsagePage struct {
	Items           []BucketEntry `json:"items"`
	NextCursor      *string       `json:"next_cursor"`
	SummaryRevision string        `json:"summary_revision"`
}

// QuoteLineInput is one line in a CreateQuote request.
type QuoteLineInput struct {
	OfferID   uuid.UUID `json:"offer_id"`
	PackCount int       `json:"pack_count"`
}

// QuoteRequest is the request body for POST /billing/quotes.
type QuoteRequest struct {
	Kind              string          `json:"kind"`
	TargetPlanVersionID *uuid.UUID    `json:"target_plan_version_id,omitempty"`
	Lines             []QuoteLineInput `json:"lines,omitempty"`
}

// QuoteLineEntry is one line in a BillingQuote response.
type QuoteLineEntry struct {
	Ordinal      int        `json:"ordinal"`
	TargetID     uuid.UUID  `json:"target_id"`
	PackCount    int        `json:"pack_count"`
	ResourceCode string     `json:"resource_code"`
	Quantity     string     `json:"quantity"`
	EffectiveAt  time.Time  `json:"effective_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	Price        Money      `json:"price"`
}

// Quote is the response for POST /billing/quotes.
type Quote struct {
	ID              uuid.UUID        `json:"id"`
	Kind            string           `json:"kind"`
	ExpiresAt       time.Time        `json:"expires_at"`
	SummaryRevision string           `json:"summary_revision"`
	RequestHash     string           `json:"request_hash"`
	Lines           []QuoteLineEntry `json:"lines"`
	Total           Money            `json:"total"`
	EffectiveAt     time.Time        `json:"effective_at"`
	ProviderAction  string           `json:"provider_action"`
}

// OrderRequest is the request body for POST /billing/orders.
type OrderRequest struct {
	QuoteID uuid.UUID `json:"quote_id"`
}

// OrderAccepted is the 202 response for POST /billing/orders.
type OrderAccepted struct {
	OrderID uuid.UUID `json:"order_id"`
	Status  string    `json:"status"`
}

// OrderAction is the current action a client should take on an order.
type OrderAction struct {
	Kind      string    `json:"kind"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Order is the full order state returned by GET /billing/orders/{order}.
type Order struct {
	ID                uuid.UUID    `json:"id"`
	Status            string       `json:"status"`
	PaymentStatus     string       `json:"payment_status"`
	FulfillmentStatus string       `json:"fulfillment_status"`
	Total             Money        `json:"total"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
	Action            *OrderAction `json:"action"`
}

// ChangeRequest is the request body for POST /billing/changes.
type ChangeRequest struct {
	QuoteID uuid.UUID `json:"quote_id"`
}

// Change is a scheduled subscription change.
type Change struct {
	ID          uuid.UUID  `json:"id"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	EffectiveAt time.Time  `json:"effective_at"`
	Revision    string     `json:"revision"`
	OrderID     *uuid.UUID `json:"order_id"`
}

// PortalRequest is the request body for POST /billing/portal.
type PortalRequest struct {
	ReturnRoute string `json:"return_route"`
}

// PortalAccepted is the 202 response for POST /billing/portal.
type PortalAccepted struct {
	PortalActionID uuid.UUID `json:"portal_action_id"`
	Status         string    `json:"status"`
}

// WebhookReceived is the response for POST /webhooks/stripe/{account}.
type WebhookReceived struct {
	Received bool `json:"received"`
}

// CatalogService reads the published billing catalog.
type CatalogService interface {
	GetCatalog(ctx context.Context, locale, currency string) (Catalog, error)
}

// BillingSummaryService reads tenant billing status.
type BillingSummaryService interface {
	GetSummary(ctx context.Context, who auth.Identity, tenantID uuid.UUID) (Summary, error)
	GetUsage(ctx context.Context, who auth.Identity, tenantID uuid.UUID, resourceCode string, limit int, cursor string) (UsageSummaryResponse, error)
	GetBuckets(ctx context.Context, who auth.Identity, tenantID uuid.UUID, resourceCode string, limit int, cursor string) (UsagePage, error)
}

// QuoteService creates billing quotes.
type QuoteService interface {
	CreateQuote(ctx context.Context, who auth.Identity, tenantID uuid.UUID, req QuoteRequest, revision int64, idempotencyKey string) (Quote, error)
}

// OrderService manages billing orders.
type OrderService interface {
	CreateOrder(ctx context.Context, who auth.Identity, tenantID uuid.UUID, req OrderRequest, revision int64, idempotencyKey string) (OrderAccepted, error)
	GetOrder(ctx context.Context, who auth.Identity, tenantID uuid.UUID, orderID uuid.UUID) (Order, error)
}

// ChangeService manages subscription changes.
type ChangeService interface {
	CreateChange(ctx context.Context, who auth.Identity, tenantID uuid.UUID, req ChangeRequest, revision int64, idempotencyKey string) (Change, error)
	CancelChange(ctx context.Context, who auth.Identity, tenantID uuid.UUID, changeID uuid.UUID, revision int64, idempotencyKey string) (Change, error)
}

// PortalService manages Stripe customer portal sessions.
type PortalService interface {
	CreatePortalSession(ctx context.Context, who auth.Identity, tenantID uuid.UUID, req PortalRequest, idempotencyKey string) (PortalAccepted, error)
}

// WebhookIngressService stores incoming Stripe webhook events.
type WebhookIngressService interface {
	ReceiveStripeWebhook(ctx context.Context, accountID uuid.UUID, rawBody []byte, signature string) (WebhookReceived, error)
}
