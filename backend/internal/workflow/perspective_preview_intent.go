package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2render"
	"time"
)

type PerspectivePreviewIntentStore struct{ DB *pgxpool.Pool }

func validatePerspectivePreviewIntent(plan PerspectivePreviewPlan) error {
	pin := plan.source
	if pin.Tenant == uuid.Nil || pin.Project == uuid.Nil || pin.Run == uuid.Nil || pin.ResultID == uuid.Nil || pin.Result.ArtifactID == uuid.Nil || pin.Attempt < 1 || pin.Fence < 1 || pin.Principal.UserID == uuid.Nil || pin.Principal.SessionID == uuid.Nil || len(plan.documents) != 2 {
		return v2input.ErrInvalid
	}
	for _, digest := range []string{plan.srgb, pin.Result.SHA256, pin.Base.Source.SHA256, pin.Base.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return v2input.ErrInvalid
		}
	}
	if err := v2render.ValidatePerspectivePreviewSource(pin); err != nil {
		return v2input.ErrInvalid
	}
	expected := uuid.NewSHA1(pin.Run, []byte("V29.PerspectivePreview:"+pin.Producer.String()+":"+pin.ResultID.String()+":"+pin.Result.SHA256+":"+plan.srgb))
	if plan.run != expected {
		return v2input.ErrInvalid
	}
	for i, variant := range []string{"preview1600", "preview2560"} {
		d := plan.documents[i]
		if d.Run != expected || d.Variant != variant || d.ID != uuid.NewSHA1(expected, []byte("render.derivative:"+variant)) || renderDerivativeBytesSHA(d.Params) != d.ParamsSHA {
			return v2input.ErrInvalid
		}
	}
	return nil
}

// Save persists only an immutable draft intent. Its transaction is committed
// before any future Params object IO. No quota or queue activation occurs here.
func (s PerspectivePreviewIntentStore) Save(ctx context.Context, plan PerspectivePreviewPlan) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if err := validatePerspectivePreviewIntent(plan); err != nil {
		return uuid.Nil, err
	}
	if s.DB == nil {
		return uuid.Nil, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return uuid.Nil, err
	}
	if role != "photo_scheduler" {
		return uuid.Nil, v2input.ErrInvalid
	}
	pin := plan.source
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", pin.Tenant.String()); err != nil {
		return uuid.Nil, err
	}
	var result *uuid.UUID
	err = tx.QueryRow(ctx, "SELECT photo.save_v29_perspective_preview_intent($1,$2,$3,$4,$5,$6,$7,$8,$9)", pin.Tenant, pin.Producer, pin.ResultID, pin.Attempt, pin.Fence, pin.Result.ArtifactID, pin.Result.SHA256, plan.srgb, plan.run).Scan(&result)
	if err != nil {
		return uuid.Nil, err
	}
	if result == nil || *result != plan.run {
		return uuid.Nil, v2input.ErrExpired
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return *result, nil
}
