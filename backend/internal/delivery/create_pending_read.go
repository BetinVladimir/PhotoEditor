package delivery

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type CreateRecovery struct {
	Found     bool
	Completed bool
	Pending   CreatePending
}

// Must run before mutable catalog resolution. Completed means the coordinator
// must use its final ticket replay path; private pending bytes are never a reply.
func (i CreateIntent) ReadPending(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID) (CreateRecovery, error) {
	return i.readPendingMode(ctx, tx, who, tid, false, false)
}

// ReadWatermarkSelectionPending restores selection evidence only; physical pins
// must be independently reauthorized before executable prepare publication.
func (i CreateIntent) ReadWatermarkSelectionPending(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID) (CreateRecovery, error) {
	return i.readPendingMode(ctx, tx, who, tid, true, false)
}

// The public coordinator can encounter either the selection-only evidence or
// its atomically promoted ordinary executable document on the same route.
func (i CreateIntent) ReadSelectedPending(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID) (CreateRecovery, error) {
	return i.readPendingMode(ctx, tx, who, tid, false, true)
}
func (i CreateIntent) readPendingMode(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID, selection, either bool) (CreateRecovery, error) {
	if err := ctx.Err(); err != nil {
		return CreateRecovery{}, err
	}
	if tx == nil || tid != i.tenant || who.UserID != i.actor || who.SessionID == uuid.Nil || i.identity.Run == uuid.Nil || i.key == "" {
		return CreateRecovery{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	var authorized bool
	if err := tx.QueryRow(ctx, `SELECT photo.v2_editor_authorized($1,$2,$3,$4)`, tid, i.request.ProjectID, who.UserID, who.SessionID).Scan(&authorized); err != nil {
		return CreateRecovery{}, err
	}
	if !authorized {
		return CreateRecovery{}, domain.NewError("NOT_FOUND", 404)
	}
	var hash string
	var status *int
	var saved []byte
	var live bool
	err := tx.QueryRow(ctx, `SELECT request_hash,response_status,response_body,expires_at>clock_timestamp() FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='POST /delivery-packages' AND key=$3`, tid, who.UserID, i.key).Scan(&hash, &status, &saved, &live)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreateRecovery{}, nil
	}
	if err != nil {
		return CreateRecovery{}, err
	}
	if hash != i.requestSHA {
		return CreateRecovery{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	}
	if status != nil {
		return CreateRecovery{Found: true, Completed: true}, nil
	}
	if !live {
		return CreateRecovery{}, domain.NewError("SNAPSHOT_EXPIRED", 410)
	}
	if len(saved) == 0 || string(saved) == "null" {
		return CreateRecovery{}, nil
	}
	pending, err := i.decodeRecoveryPending(ctx, who.SessionID, saved, selection, either)
	if err != nil {
		return CreateRecovery{}, err
	}
	refs := make([]v2input.Ref, len(pending.Items))
	for index, item := range pending.Items {
		refs[index] = item.Recipe
	}
	// Verify live source/recipe eligibility without rereading current preset config.
	current, err := i.ResolveSelection(ctx, tx, who, tid, refs)
	if err != nil {
		return CreateRecovery{}, err
	}
	if !sameCreateSelection(current, pending.Items) {
		return CreateRecovery{}, domain.NewError("DELIVERY_SOURCE_STALE", 409)
	}
	return CreateRecovery{Found: true, Pending: pending}, nil
}
func (i CreateIntent) decodeRecoveryPending(ctx context.Context, session uuid.UUID, saved []byte, selection, either bool) (CreatePending, error) {
	pending, err := i.decodePendingProjection(ctx, session, saved, nil, false, selection)
	if err != nil && either && domain.AsError(err).Code == "DELIVERY_PENDING_INVALID" {
		return i.decodePendingProjection(ctx, session, saved, nil, false, true)
	}
	return pending, err
}
func (s Service) RecoverCreate(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string) (CreateRecovery, error) {
	return s.recoverCreate(ctx, who, tid, request, key, false)
}
func (s Service) RecoverWatermarkSelection(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string) (CreateRecovery, error) {
	return s.recoverCreate(ctx, who, tid, request, key, true)
}
func (s Service) RecoverSelectedCreate(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string) (CreateRecovery, error) {
	return s.recoverCreateMode(ctx, who, tid, request, key, false, true)
}
func (s Service) recoverCreate(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, selection bool) (CreateRecovery, error) {
	return s.recoverCreateMode(ctx, who, tid, request, key, selection, false)
}
func (s Service) recoverCreateMode(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, selection, either bool) (CreateRecovery, error) {
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return CreateRecovery{}, err
	}
	if s.DB == nil {
		return CreateRecovery{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	var result CreateRecovery
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		result, err = intent.readPendingMode(ctx, tx, who, tid, selection, either)
		return err
	})
	return result, err
}

func sameCreateSelection(a, b []v2input.SessionSnapshotItem) bool {
	left, err := canonical.Marshal(a)
	if err != nil {
		return false
	}
	right, err := canonical.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}
