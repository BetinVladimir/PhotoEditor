package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s *ReflectionSubmissionService) ListMaskPairs(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query GlareMaskPairQuery) (GlareMaskPairPage, error) {
	var zero GlareMaskPairPage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(query)
	if err != nil {
		return zero, err
	}
	query, err = DecodeGlareMaskPairQuery(body)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.scheduler.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(query.Base)
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx,
		"SELECT photo.list_v28_glare_mask_pairs($1,$2,$3,$4,$5,$6::jsonb,$7)",
		tenant, project, query.Snapshot, who.UserID, who.SessionID,
		string(base), query.After).Scan(&raw); err != nil {
		return zero, err
	}
	page, err := decodeGlareMaskPairPage(raw, query)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return page, nil
}
