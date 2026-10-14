package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2render"
)

type PerspectiveQualityIntentStore struct{ DB *pgxpool.Pool }

func validatePerspectiveQualityIntent(plan PerspectiveQualityPlan) error {
	pin := plan.source
	if pin.Tenant == uuid.Nil || pin.Project == uuid.Nil || pin.Run == uuid.Nil || pin.Workflow == uuid.Nil || pin.Producer == uuid.Nil || pin.ResultID == uuid.Nil || pin.Result.ArtifactID == uuid.Nil || pin.Base.AssetVersionID == uuid.Nil || pin.Base.Source.ArtifactID == uuid.Nil || pin.Principal.UserID == uuid.Nil || pin.Principal.SessionID == uuid.Nil || pin.Attempt < 1 || pin.Fence < 1 {
		return v2input.ErrInvalid
	}
	for _, sha := range []string{pin.Result.SHA256, pin.Base.Source.SHA256, pin.Base.CanvasHash} {
		if domainDigestInvalid(sha) {
			return v2input.ErrInvalid
		}
	}
	if err := v2render.ValidatePerspectivePreviewSource(pin); err != nil {
		return v2input.ErrInvalid
	}
	// Reuse exact plan/task/Params/profile validation without authorizing IO.
	params := review.ArtifactRef{ID: uuid.NewSHA1(plan.run, []byte("quality.params.validation")), SHA256: renderDerivativeBytesSHA(plan.params)}
	_, err := PreparePerspectiveQualityTask(plan, params)
	return err
}
func (s PerspectiveQualityIntentStore) Save(ctx context.Context, plan PerspectiveQualityPlan) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if err := validatePerspectiveQualityIntent(plan); err != nil {
		return uuid.Nil, err
	}
	var saved *uuid.UUID
	err := withPerspectivePreviewDocumentTx(ctx, s.DB, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		p := plan.source
		return tx.QueryRow(ctx, "SELECT photo.save_v29_perspective_quality_intent($1,$2,$3,$4,$5,$6,$7,$8)", p.Tenant, p.Producer, p.ResultID, p.Attempt, p.Fence, p.Result.ArtifactID, p.Result.SHA256, plan.run).Scan(&saved)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if saved == nil || *saved != plan.run {
		return uuid.Nil, v2input.ErrExpired
	}
	return *saved, nil
}
