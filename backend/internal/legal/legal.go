// Package legal provides service interfaces for legal requirements and acceptances.
package legal

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

// DocumentEntry is one legal document in a requirement set.
type DocumentEntry struct {
	ID           uuid.UUID `json:"id"`
	Key          string    `json:"key"`
	Version      string    `json:"version"`
	URL          string    `json:"url"`
	ContentSHA256 string   `json:"content_sha256"`
	RequiredAction string  `json:"required_action"`
}

// OptionalPurpose is an optional consent purpose.
type OptionalPurpose struct {
	Code             string `json:"code"`
	EnabledByDefault bool   `json:"enabled_by_default"`
}

// RequirementSet is the response for GET /legal/requirements.
type RequirementSet struct {
	SetHash          string            `json:"set_hash"`
	Locale           string            `json:"locale"`
	Documents        []DocumentEntry   `json:"documents"`
	OptionalPurposes []OptionalPurpose `json:"optional_purposes"`
}

// AcceptanceEvent is a single acceptance action within a legal acceptance.
type AcceptanceEvent struct {
	DocumentVersionID uuid.UUID `json:"document_version_id"`
	Action            string    `json:"action"`
}

// Acceptance is the request body for POST /me/legal/acceptances.
type Acceptance struct {
	DocumentSetHash string            `json:"document_set_hash"`
	Events          []AcceptanceEvent `json:"events"`
	ClientEventID   string            `json:"client_event_id"`
}

// AcceptanceResult is the response for POST /me/legal/acceptances.
type AcceptanceResult struct {
	OnboardingState string `json:"onboarding_state"`
	AcceptedSetHash string `json:"accepted_set_hash"`
	Revision        string `json:"revision"`
}

// Service provides legal document and acceptance operations.
type Service interface {
	GetRequirements(ctx context.Context, locale, accountKind string) (RequirementSet, error)
	RecordAcceptance(ctx context.Context, who auth.Identity, req Acceptance, idempotencyKey string) (AcceptanceResult, error)
}

// Validate checks that the acceptance request is well-formed.
func (a Acceptance) Validate() error {
	if len(a.DocumentSetHash) == 0 {
		return errMissingField("document_set_hash")
	}
	if len(a.Events) == 0 {
		return errMissingField("events")
	}
	if len(a.ClientEventID) == 0 || len(a.ClientEventID) > 128 {
		return errMissingField("client_event_id")
	}
	for _, e := range a.Events {
		if e.DocumentVersionID == uuid.Nil {
			return errMissingField("document_version_id")
		}
		if e.Action != "accepted" && e.Action != "acknowledged" {
			return errInvalidField("action")
		}
	}
	return nil
}

// Validate checks that the requirement query parameters are present.
func ValidateRequirementsQuery(locale, accountKind string) error {
	switch locale {
	case "en", "ru", "bg":
	default:
		return errInvalidField("locale")
	}
	switch accountKind {
	case "personal", "business":
	default:
		return errInvalidField("account_kind")
	}
	return nil
}

type fieldError struct {
	field string
	msg   string
}

func (e fieldError) Error() string { return e.msg + ": " + e.field }

func errMissingField(f string) error  { return fieldError{f, "missing required field"} }
func errInvalidField(f string) error  { return fieldError{f, "invalid field value"} }

// _ satisfies unused import for time
var _ = time.Time{}
