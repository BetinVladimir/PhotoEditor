package billing

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/domain"
)

func pgErr(msg string) error {
	return &pgconn.PgError{Message: msg}
}
func pgLockErr() error {
	return &pgconn.PgError{Code: "55P03", Message: "lock_not_available"}
}

func TestMapError_nil(t *testing.T) {
	if MapError(nil) != nil {
		t.Fatal("nil input must return nil")
	}
}

func TestMapError_nonPg(t *testing.T) {
	plain := errors.New("plain")
	if MapError(plain) != plain {
		t.Fatal("non-pg error must pass through")
	}
}

func TestMapError_lockTimeout(t *testing.T) {
	de := domain.AsError(MapError(pgLockErr()))
	if de.Code != "BILLING_UNAVAILABLE" || de.HTTPStatus != 503 {
		t.Fatalf("55P03 → BILLING_UNAVAILABLE 503, got %s %d", de.Code, de.HTTPStatus)
	}
}

var mapErrorTable = []struct {
	msg    string
	code   string
	status int
}{
	// pre-existing
	{"BILLING_QUOTA_EXCEEDED", "BILLING_QUOTA_EXCEEDED", 409},
	{"BILLING_OVERRUN_BLOCKED", "BILLING_QUOTA_EXCEEDED", 409},
	{"BILLING_BETA_BUDGET_EXCEEDED", "BILLING_QUOTA_EXCEEDED", 409},
	{"PHOTO_INITIAL_IN_PROGRESS", "PHOTO_INITIAL_IN_PROGRESS", 409},
	{"BILLING_IDEMPOTENCY_CONFLICT", "IDEMPOTENCY_CONFLICT", 409},
	{"BILLING_AUTHORITY_INVALID", "FORBIDDEN", 403},
	{"BILLING_OPERATION_POLICY_REQUIRED", "BILLING_UNAVAILABLE", 503},
	{"BILLING_UNAVAILABLE", "BILLING_UNAVAILABLE", 503},
	{"BILLING_PERIOD_INVALID", "BILLING_UNAVAILABLE", 503},
	{"BILLING_ACCOUNTING_DISABLED", "BILLING_UNAVAILABLE", 503},
	{"BILLING_POLICY_INVALID", "BILLING_UNAVAILABLE", 503},
	{"BILLING_PERIOD_POLICY_PINNED", "BILLING_UNAVAILABLE", 503},
	// Group B — OPEN-11 decision
	{"BILLING_BATCH_INVALID", "VALIDATION_FAILED", 422},
	{"BILLING_CHILD_INVALID", "VALIDATION_FAILED", 422},
	{"BILLING_CHILD_NOT_ROOT", "VALIDATION_FAILED", 422},
	{"BILLING_REQUEST_INVALID", "VALIDATION_FAILED", 422},
	{"BILLING_RESOURCE_INVALID", "VALIDATION_FAILED", 422},
	{"BILLING_ROUTE_INVALID", "VALIDATION_FAILED", 422},
	{"BILLING_RUN_INVALID", "VALIDATION_FAILED", 422},
	{"BILLING_SOURCE_INVALID", "VALIDATION_FAILED", 422},
	// Group C — OPEN-11 decision
	{"BILLING_CHILD_ALREADY_CHARGED", "BILLING_STATE_CONFLICT", 409},
	{"BILLING_GRAPH_REFUSED", "BILLING_STATE_CONFLICT", 409},
	{"BILLING_LEASE_EXPIRED", "BILLING_STATE_CONFLICT", 409},
	{"BILLING_LOCAL_LEASE_LIMIT", "BILLING_STATE_CONFLICT", 409},
	{"BILLING_LOCAL_REVIEW_FINALIZATION_RACE", "BILLING_STATE_CONFLICT", 409},
	{"BILLING_RUN_TERMINAL", "BILLING_STATE_CONFLICT", 409},
	{"BILLING_STALE_GENERATION", "BILLING_STATE_CONFLICT", 409},
	{"BILLING_TENANT_INACTIVE", "BILLING_STATE_CONFLICT", 409},
	// Group D — OPEN-11 decision
	{"BILLING_OPERATION_PROOF_UNAVAILABLE", "BILLING_UNAVAILABLE", 503},
	{"BILLING_PAID_OPERATION_HELPER_OCCUPIED", "BILLING_UNAVAILABLE", 503},
	{"BILLING_RENDER_PROOF_UNAVAILABLE", "BILLING_UNAVAILABLE", 503},
}

func TestMapError_table(t *testing.T) {
	for _, tc := range mapErrorTable {
		err := MapError(pgErr(tc.msg))
		de := domain.AsError(err)
		if de.Code != tc.code || de.HTTPStatus != tc.status {
			t.Errorf("MapError(%q) = %s %d, want %s %d", tc.msg, de.Code, de.HTTPStatus, tc.code, tc.status)
		}
	}
}

func TestMapError_unknown_passes_through(t *testing.T) {
	raw := pgErr("BILLING_UNKNOWN_CODE_XYZ")
	if MapError(raw) != raw {
		t.Fatal("unknown pg error must pass through unchanged")
	}
}
