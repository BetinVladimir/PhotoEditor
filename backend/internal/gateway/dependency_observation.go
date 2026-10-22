package gateway

import (
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
	"photoeditor/backend/internal/objectstore"
)

// dependencyObservation deliberately excludes error text, URLs, keys, SQL,
// parameters, task capabilities and all request headers.
func dependencyObservation(stage string, err error) {
	if err == nil || errors.Is(err, ErrCapability) || errors.Is(err, objectstore.ErrMissing) {
		return
	}
	category := "dependency"
	if errors.Is(err, objectstore.ErrIntegrity) {
		category = "object_integrity"
	}
	if errors.Is(err, objectstore.ErrInvalid) {
		category = "object_reference_invalid"
	}
	attrs := []any{"stage", stage, "category", category, "error_type", fmt.Sprintf("%T", err)}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && len(pgerr.Code) == 5 {
		valid := true
		for _, c := range pgerr.Code {
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z') {
				valid = false
			}
		}
		if valid {
			attrs = append(attrs, "sqlstate", pgerr.Code)
		}
	}
	slog.Warn("gateway dependency unavailable", attrs...)
}
