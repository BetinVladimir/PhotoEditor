// Package billing connects durable funding to user-level workflow boundaries.
// It performs no payment/provider or object-store I/O.
package billing

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// Each process owns its finite DB-backed leases; restarts cannot reset period budgets.
var instanceID = uuid.NewString()

func PrepareApply(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, who auth.Identity, route string) (bool, error) {
	// Take billing locks after the API receipt, before collection/run/quota locks.
	cold, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	var enabled bool
	err := tx.QueryRow(cold, `SELECT photo.billing_prepare_apply($1,$2,$3,$4)`, tenantID, who.UserID, who.SessionID, route).Scan(&enabled)
	return enabled, checkColdLock(ctx, cold, err)
}

func BindApply(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, who auth.Identity, runID uuid.UUID, key string, route string, request any) error {
	hash, err := canonical.Hash(request)
	if err != nil {
		return err
	}
	digest, err := hex.DecodeString(hash)
	if err != nil {
		return err
	}
	var requestID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT photo.billing_bind_routed_apply($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, who.UserID, who.SessionID, runID, key, digest, instanceID, route).Scan(&requestID)
	return MapError(err)
}

func MapError(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	if pg.Code == "55P03" {
		return domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	switch pg.Message {
	case "BILLING_QUOTA_EXCEEDED", "BILLING_OVERRUN_BLOCKED", "BILLING_BETA_BUDGET_EXCEEDED":
		return domain.NewError("BILLING_QUOTA_EXCEEDED", 409)
	case "PHOTO_INITIAL_IN_PROGRESS":
		return domain.NewError("PHOTO_INITIAL_IN_PROGRESS", 409)
	case "BILLING_IDEMPOTENCY_CONFLICT":
		return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "BILLING_AUTHORITY_INVALID":
		return domain.NewError("FORBIDDEN", 403)
	// Group B (OPEN-11): invalid request → 422 VALIDATION_FAILED
	case "BILLING_BATCH_INVALID", "BILLING_CHILD_INVALID", "BILLING_CHILD_NOT_ROOT",
		"BILLING_REQUEST_INVALID", "BILLING_RESOURCE_INVALID", "BILLING_ROUTE_INVALID",
		"BILLING_RUN_INVALID", "BILLING_SOURCE_INVALID":
		return domain.NewError("VALIDATION_FAILED", 422)
	// Group C (OPEN-11): state conflict → 409 BILLING_STATE_CONFLICT
	case "BILLING_CHILD_ALREADY_CHARGED", "BILLING_GRAPH_REFUSED", "BILLING_LEASE_EXPIRED",
		"BILLING_LOCAL_LEASE_LIMIT", "BILLING_LOCAL_REVIEW_FINALIZATION_RACE",
		"BILLING_RUN_TERMINAL", "BILLING_STALE_GENERATION", "BILLING_TENANT_INACTIVE":
		return domain.NewError("BILLING_STATE_CONFLICT", 409)
	// Group D (OPEN-11) + existing unavailable codes → 503 BILLING_UNAVAILABLE
	case "BILLING_OPERATION_POLICY_REQUIRED", "BILLING_UNAVAILABLE",
		"BILLING_PERIOD_INVALID", "BILLING_ACCOUNTING_DISABLED", "BILLING_POLICY_INVALID",
		"BILLING_PERIOD_POLICY_PINNED", "BILLING_OPERATION_PROOF_UNAVAILABLE",
		"BILLING_PAID_OPERATION_HELPER_OCCUPIED", "BILLING_RENDER_PROOF_UNAVAILABLE":
		return domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	return err
}

// checkColdLock converts a DB lock-wait timeout to BILLING_UNAVAILABLE.
// ctx is the parent context; cold is the short-deadline child used for the query.
func checkColdLock(ctx, cold context.Context, err error) error {
	if err != nil && ctx.Err() == nil && errors.Is(cold.Err(), context.DeadlineExceeded) {
		return domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	return MapError(err)
}
