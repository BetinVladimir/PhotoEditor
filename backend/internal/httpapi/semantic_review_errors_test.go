package httpapi

import (
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/domain"
	"testing"
)

func TestReviewSQLAdmissionErrorsHaveStablePublicCodes(t *testing.T) {
	for _, test := range []struct {
		sql, message, code string
		status             int
	}{
		{"42501", "SEMANTIC_REVIEW_FORBIDDEN", "NOT_FOUND", 404},
		{"42501", "SEMANTIC_CLOSE_FORBIDDEN", "NOT_FOUND", 404},
		{"40001", "SEMANTIC_REVIEW_INCOMPLETE", "REVIEW_INCOMPLETE", 409},
		{"40001", "SEMANTIC_CLOSE_STALE", "REVISION_CONFLICT", 409},
	} {
		err := fmt.Errorf("private database detail: %w", &pgconn.PgError{Code: test.sql, Message: test.message})
		public := domain.AsError(semanticReviewError(err))
		if public.Code != test.code || public.HTTPStatus != test.status {
			t.Fatal(test, public)
		}
	}
	for _, sql := range []string{"42501", "23514", "08006"} {
		original := &pgconn.PgError{Code: sql, Message: "permission denied or infrastructure failure"}
		if semanticReviewError(original) != original {
			t.Fatal("unexpected database failure disguised as admission conflict", sql)
		}
	}
}
