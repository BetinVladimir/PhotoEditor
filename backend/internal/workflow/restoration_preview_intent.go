package workflow

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type RestorationPreviewIntentStore struct{ DB *pgxpool.Pool }

func validateRestorationPreviewIntent(plan RestorationPreviewPlan) error {
	if validateRestorationPreviewSource(plan.source) != nil || plan.run == uuid.Nil {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(plan.srgb); err != nil {
		return v2input.ErrInvalid
	}
	pin := plan.source
	expected := uuid.NewSHA1(pin.Run, []byte("V28.RestorationPreview:"+
		pin.Producer.String()+":"+pin.ResultID.String()+":"+
		pin.Result.SHA256+":"+pin.Review.String()+":"+plan.srgb))
	if plan.run != expected {
		return v2input.ErrInvalid
	}
	for i, side := range []string{"before", "after"} {
		image := pin.Base.Source
		if i == 1 {
			image = pin.Candidate
		}
		for j, variant := range []string{"preview1600", "preview2560"} {
			doc := plan.docs[i*2+j]
			if doc.Side != side || doc.Run != expected || doc.Variant != variant ||
				doc.ID != restorationPreviewTaskID(expected, side, variant) ||
				renderDerivativeBytesSHA(doc.Params) != doc.ParamsSHA ||
				len(doc.Inputs) < 2 || doc.Inputs[0].ID != image.ArtifactID ||
				doc.Inputs[0].SHA256 != image.SHA256 {
				return v2input.ErrInvalid
			}
			if _, err := PrepareRestorationPreviewTask(doc,
				review.ArtifactRef{ID: uuid.NewSHA1(doc.ID, []byte("intent.params")),
					SHA256: doc.ParamsSHA}); err != nil {
				return v2input.ErrInvalid
			}
		}
	}
	return nil
}

// Save freezes source and four-sided plan identity. Its commit precedes object
// publication and does not imply a runnable or user-visible preview graph.
func (s RestorationPreviewIntentStore) Save(ctx context.Context, plan RestorationPreviewPlan) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if err := validateRestorationPreviewIntent(plan); err != nil || s.DB == nil {
		return uuid.Nil, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return uuid.Nil, err
	}
	if role != "photo_scheduler" {
		return uuid.Nil, v2input.ErrInvalid
	}
	pin := plan.source
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", pin.Tenant.String()); err != nil {
		return uuid.Nil, err
	}
	var result *uuid.UUID
	err = tx.QueryRow(bounded,
		"SELECT photo.save_v28_restoration_preview_intent($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)",
		pin.Tenant, pin.Producer, pin.Review, pin.ResultID, pin.Attempt,
		pin.Fence, pin.Result.ArtifactID, pin.Result.SHA256, plan.srgb, plan.run).Scan(&result)
	if err != nil {
		return uuid.Nil, err
	}
	if result == nil || *result != plan.run {
		return uuid.Nil, v2input.ErrExpired
	}
	if err = tx.Commit(bounded); err != nil {
		return uuid.Nil, err
	}
	return *result, nil
}
