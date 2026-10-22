package delivery

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// promoteOrdinarySelection replaces selection-only evidence with the executable
// ordinary pending document under the same idempotency lock. No object effect
// may occur until the transaction commits.
func (s Service) promoteOrdinarySelection(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, selected CreatePending) error {
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return err
	}
	payload, err := intent.EncodePending(ctx, who.SessionID, selected.Catalog, selected.Items)
	if err != nil {
		return err
	}
	return tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT photo.v2_editor_authorized($1,$2,$3,$4)`, tid, request.ProjectID, who.UserID, who.SessionID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return domain.NewError("NOT_FOUND", 404)
		}
		var hash string
		var status *int
		var saved []byte
		var live bool
		if err := tx.QueryRow(ctx, `SELECT request_hash,response_status,response_body,expires_at>clock_timestamp() FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='POST /delivery-packages' AND key=$3 FOR UPDATE`, tid, who.UserID, key).Scan(&hash, &status, &saved, &live); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT photo.v2_editor_authorized($1,$2,$3,$4)`, tid, request.ProjectID, who.UserID, who.SessionID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return domain.NewError("NOT_FOUND", 404)
		}
		if hash != intent.requestSHA {
			return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		}
		if status != nil || !live {
			return domain.NewError("DELIVERY_CREATE_STALE", 409)
		}
		// Another identical caller may have promoted this row while we waited
		// for its lock. Exact executable bytes are an idempotent success.
		if bytes.Equal(saved, payload) {
			if _, err := intent.DecodePending(ctx, who.SessionID, saved); err != nil {
				return err
			}
			return ctx.Err()
		}
		current, err := intent.DecodeWatermarkSelectionPending(ctx, who.SessionID, saved)
		if err != nil {
			return err
		}
		currentRaw, err := canonical.Marshal(current)
		if err != nil {
			return err
		}
		selectedRaw, err := canonical.Marshal(selected)
		if err != nil || !bytes.Equal(currentRaw, selectedRaw) {
			return domain.NewError("DELIVERY_SOURCE_STALE", 409)
		}
		refs := make([]v2input.Ref, len(current.Items))
		for index, item := range current.Items {
			refs[index] = item.Recipe
		}
		items, err := intent.ResolveSelection(ctx, tx, who, tid, refs)
		if err != nil {
			return err
		}
		if !sameCreateSelection(items, current.Items) {
			return domain.NewError("DELIVERY_SOURCE_STALE", 409)
		}
		_, err = tx.Exec(ctx, `UPDATE photo.idempotency_records SET response_body=$4 WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='POST /delivery-packages' AND key=$3 AND response_status IS NULL`, tid, who.UserID, key, payload)
		return err
	})
}
