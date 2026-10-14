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

type MultiQualityIntentStore struct{ DB *pgxpool.Pool }

func validateMultiQualityIntent(plan MultiQualityPlan) error {
	expected, err := v2render.MultiQualityRunID(plan.source)
	if err != nil || expected != plan.run {
		return v2input.ErrInvalid
	}
	params := review.ArtifactRef{ID: uuid.NewSHA1(plan.run, []byte("quality.params.validation")), SHA256: renderDerivativeBytesSHA(plan.params)}
	_, err = PrepareMultiQualityTask(plan, params)
	return err
}

// Save creates only immutable draft intent, never quota or dispatch authority.
func (s MultiQualityIntentStore) Save(ctx context.Context, plan MultiQualityPlan) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if err := validateMultiQualityIntent(plan); err != nil {
		return uuid.Nil, err
	}
	var saved *uuid.UUID
	err := withMultiPreviewDocumentTx(ctx, s.DB, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		p := plan.source
		return tx.QueryRow(ctx, "SELECT photo.save_v29_multi_quality_intent($1,$2,$3,$4,$5,$6,$7,$8,$9)", p.Tenant, p.Producer, p.Binding.Operation, p.Binding.ResultID, p.Attempt, p.Fence, p.Binding.Result.ArtifactID, p.Binding.Result.SHA256, plan.run).Scan(&saved)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if saved == nil || *saved != plan.run {
		return uuid.Nil, v2input.ErrExpired
	}
	return *saved, nil
}
