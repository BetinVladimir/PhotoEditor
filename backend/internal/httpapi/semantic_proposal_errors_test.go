package httpapi

import (
	"errors"
	"fmt"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestProposalAdmissionFailuresHaveStablePublicCodes(t *testing.T) {
	for _, test := range []struct {
		cause  error
		code   string
		status int
	}{
		{v2input.ErrInvalid, "VALIDATION_FAILED", 422},
		{v2input.ErrExpired, "SNAPSHOT_EXPIRED", 410},
		{artifactjson.ErrStale, "SNAPSHOT_EXPIRED", 410},
		{v2input.ErrConflict, "IDEMPOTENCY_CONFLICT", 409},
		{artifactjson.ErrConflict, "IDEMPOTENCY_CONFLICT", 409},
		{artifactjson.ErrQuota, "QUOTA_EXCEEDED", 409},
		{v2qualification.ErrUnqualified, "DEPENDENCY_UNAVAILABLE", 503},
	} {
		public := domain.AsError(semanticProposalError(fmt.Errorf("private detail: %w", test.cause)))
		if public.Code != test.code || public.HTTPStatus != test.status {
			t.Fatal(test, public)
		}
	}
	infrastructure := errors.New("database connection failure")
	if semanticProposalError(infrastructure) != infrastructure {
		t.Fatal("infrastructure failure disguised as scope expiration")
	}
}
