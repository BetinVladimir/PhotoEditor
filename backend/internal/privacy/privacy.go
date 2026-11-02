// Package privacy provides service interfaces for privacy preferences and account deletion.
package privacy

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

// Preferences is the current state of a user's privacy preferences.
type Preferences struct {
	Revision  string    `json:"revision"`
	Analytics bool      `json:"analytics"`
	Marketing bool      `json:"marketing"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PreferencesUpdate is the request body for PATCH /me/privacy.
// At least one field must be set.
type PreferencesUpdate struct {
	Analytics *bool `json:"analytics,omitempty"`
	Marketing *bool `json:"marketing,omitempty"`
}

// Validate checks that at least one field is present.
func (p PreferencesUpdate) Validate() error {
	if p.Analytics == nil && p.Marketing == nil {
		return errMissingField("analytics or marketing")
	}
	return nil
}

// OwnershipResolution is one tenant ownership decision in a deletion request.
type OwnershipResolution struct {
	TenantID          uuid.UUID  `json:"tenant_id"`
	TenantRevision    string     `json:"tenant_revision"`
	Action            string     `json:"action"`
	TransferReceiptID *uuid.UUID `json:"transfer_receipt_id,omitempty"`
}

// DeletionRequest is the request body for POST /me/deletion.
type DeletionRequest struct {
	PrivacyCaseID       uuid.UUID             `json:"privacy_case_id"`
	RecentAuthGrant     string                `json:"recent_auth_grant"`
	OwnershipResolution []OwnershipResolution `json:"ownership_resolution"`
}

// DeletionAccepted is the 202 response for POST /me/deletion.
type DeletionAccepted struct {
	RequestID        uuid.UUID `json:"request_id"`
	Status           string    `json:"status"`
	DeadlineAt       time.Time `json:"deadline_at"`
	StatusCapability string    `json:"status_capability"`
	StatusExpiresAt  time.Time `json:"status_expires_at"`
}

// DeletionStatusStep is one step in a deletion status response.
type DeletionStatusStep struct {
	Category string `json:"category"`
	State    string `json:"state"`
}

// RetainedCategory describes data retained after deletion with its deadline.
type RetainedCategory struct {
	Category string    `json:"category"`
	Deadline time.Time `json:"deadline"`
}

// DeletionStatus is the response for GET /me/deletion/{request}/status.
type DeletionStatus struct {
	State              string               `json:"state"`
	Steps              []DeletionStatusStep `json:"steps"`
	RetainedCategories []RetainedCategory   `json:"retained_categories"`
}

// Validate checks that the deletion request is well-formed.
func (d DeletionRequest) Validate() error {
	if d.PrivacyCaseID == uuid.Nil {
		return errMissingField("privacy_case_id")
	}
	if len(d.RecentAuthGrant) == 0 {
		return errMissingField("recent_auth_grant")
	}
	for _, r := range d.OwnershipResolution {
		if r.TenantID == uuid.Nil {
			return errMissingField("tenant_id")
		}
		if r.Action != "close" && r.Action != "transfer" {
			return errInvalidField("action")
		}
	}
	return nil
}

// PreferencesService updates optional consent independently of deletion readiness.
type PreferencesService interface {
	UpdatePreferences(ctx context.Context, who auth.Identity, req PreferencesUpdate, revision int64, idempotencyKey string) (Preferences, error)
}

// Service provides privacy preference and deletion request operations.
type Service interface {
	PreferencesService
	RequestDeletion(ctx context.Context, who auth.Identity, req DeletionRequest, idempotencyKey string) (DeletionAccepted, error)
	GetDeletionStatus(ctx context.Context, requestID uuid.UUID, statusToken string) (DeletionStatus, error)
}

type fieldError struct {
	field string
	msg   string
}

func (e fieldError) Error() string { return e.msg + ": " + e.field }

func errMissingField(f string) error { return fieldError{f, "missing required field"} }
func errInvalidField(f string) error { return fieldError{f, "invalid field value"} }
