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
	if err != nil && ctx.Err() == nil && errors.Is(cold.Err(), context.DeadlineExceeded) {
		return false, domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	return enabled, MapError(err)
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
	case "BILLING_OPERATION_POLICY_REQUIRED", "BILLING_UNAVAILABLE",
		"BILLING_PERIOD_INVALID", "BILLING_ACCOUNTING_DISABLED", "BILLING_POLICY_INVALID", "BILLING_PERIOD_POLICY_PINNED":
		return domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	return err
}
