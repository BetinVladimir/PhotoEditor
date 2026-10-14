package httpapi

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/semanticreview"
)

// Translate only declared review admission failures. Database infrastructure
// and privilege configuration failures retain internal error handling.
func semanticReviewError(err error) error {
	switch {
	case errors.Is(err, semanticreview.ErrUnavailable):
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	case errors.Is(err, semanticreview.ErrReviewIncomplete):
		return domain.NewError("REVIEW_INCOMPLETE", 409)
	case errors.Is(err, semanticreview.ErrConflict):
		return domain.NewError("REVISION_CONFLICT", 409)
	case errors.Is(err, semanticreview.ErrInvalid):
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	var sqlError *pgconn.PgError
	if !errors.As(err, &sqlError) {
		return err
	}
	switch {
	case sqlError.Code == "42501" && strings.HasPrefix(sqlError.Message, "SEMANTIC_") && strings.HasSuffix(sqlError.Message, "_FORBIDDEN"):
		return domain.NewError("NOT_FOUND", 404)
	case sqlError.Code == "40001" && sqlError.Message == "SEMANTIC_REVIEW_INCOMPLETE":
		return domain.NewError("REVIEW_INCOMPLETE", 409)
	case sqlError.Code == "40001":
		return domain.NewError("REVISION_CONFLICT", 409)
	default:
		return err
	}
}
