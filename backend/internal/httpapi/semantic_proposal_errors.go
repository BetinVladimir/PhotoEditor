package httpapi

import (
	"errors"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Only declared proposal failures receive public codes; infrastructure errors
// retain their existing handling and never expose private DB/object details.
func semanticProposalError(err error) error {
	switch {
	case errors.Is(err, v2input.ErrInvalid):
		return domain.NewError("VALIDATION_FAILED", 422)
	case errors.Is(err, v2input.ErrExpired), errors.Is(err, artifactjson.ErrStale):
		return domain.NewError("SNAPSHOT_EXPIRED", 410)
	case errors.Is(err, v2input.ErrConflict), errors.Is(err, artifactjson.ErrConflict):
		return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case errors.Is(err, artifactjson.ErrQuota):
		return domain.NewError("QUOTA_EXCEEDED", 409)
	case errors.Is(err, v2qualification.ErrUnqualified):
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	default:
		return semanticReviewError(err)
	}
}
