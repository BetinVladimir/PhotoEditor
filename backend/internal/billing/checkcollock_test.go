package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/domain"
)

func TestCheckColdLock_nilError(t *testing.T) {
	ctx := context.Background()
	cold, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := checkColdLock(ctx, cold, nil); err != nil {
		t.Fatalf("nil err must return nil, got %v", err)
	}
}

func TestCheckColdLock_parentCancelledPassesThrough(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	parentCancel() // parent cancelled
	cold, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	raw := errors.New("some db error")
	err := checkColdLock(parent, cold, raw)
	if err != raw {
		t.Fatalf("parent-cancelled context must pass through MapError(raw), got %v", err)
	}
}

func TestCheckColdLock_coldDeadlineExceededReturnsBillingUnavailable(t *testing.T) {
	ctx := context.Background()
	// create a cold context that is already past its deadline
	cold, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()

	raw := &pgconn.PgError{Code: "57014", Message: "query_canceled"}
	err := checkColdLock(ctx, cold, raw)
	de := domain.AsError(err)
	if de.Code != "BILLING_UNAVAILABLE" || de.HTTPStatus != 503 {
		t.Fatalf("cold deadline exceeded must return BILLING_UNAVAILABLE 503, got %s %d", de.Code, de.HTTPStatus)
	}
}

func TestCheckColdLock_nonDeadlineErrPassesThrough(t *testing.T) {
	ctx := context.Background()
	cold, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	// cold did NOT time out, so Err() == nil

	raw := &pgconn.PgError{Message: "BILLING_UNAVAILABLE"}
	err := checkColdLock(ctx, cold, raw)
	de := domain.AsError(err)
	// MapError maps BILLING_UNAVAILABLE message → BILLING_UNAVAILABLE 503
	if de.Code != "BILLING_UNAVAILABLE" || de.HTTPStatus != 503 {
		t.Fatalf("non-deadline err must go through MapError, got %s %d", de.Code, de.HTTPStatus)
	}
}

func TestCheckColdLock_unknownPgErrorPassesThrough(t *testing.T) {
	ctx := context.Background()
	cold, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	raw := &pgconn.PgError{Code: "42601", Message: "syntax_error"}
	err := checkColdLock(ctx, cold, raw)
	if err != raw {
		t.Fatalf("unknown pg error must pass through unchanged, got %v", err)
	}
}
