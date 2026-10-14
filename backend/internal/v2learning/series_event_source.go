package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// SeriesProducerCandidate comes from the unique, currently authorized Apply
// path in SQL. It is a source identity, not artifact-read or task authority.
type SeriesProducerCandidate struct {
	AssetVersionID uuid.UUID
	ConsentID      uuid.UUID
	ConsentEpoch   int64
	ApplyRunID     uuid.UUID
	ItemID         uuid.UUID
	ProducerTaskID uuid.UUID
	SourceImage    v2input.Ref
	StageImage     v2input.Ref
	Params         v2input.Ref
	Series         v2input.Ref
}

func validSeriesProducerCandidate(tenantID uuid.UUID, source SeriesProducerCandidate,
	candidate TrainingCandidate, render TrainingRenderCase) bool {
	if candidate.EventID == uuid.Nil || !candidate.ConsentLive ||
		!candidate.SourceReady || !candidate.BaseReady || !candidate.ApprovedReady ||
		source.AssetVersionID == uuid.Nil ||
		source.AssetVersionID != candidate.AssetVersionID ||
		source.AssetVersionID != render.Binding.AssetVersionID ||
		source.ConsentID == uuid.Nil || source.ConsentID != candidate.ConsentID ||
		source.ConsentEpoch < 1 || source.ConsentEpoch != candidate.ConsentEpoch ||
		source.ApplyRunID == uuid.Nil || source.ItemID == uuid.Nil ||
		source.ProducerTaskID == uuid.Nil ||
		source.SourceImage != render.WorkingImage ||
		source.SourceImage != render.Binding.Source ||
		source.SourceImage.SHA256 != candidate.SourceContentSHA {
		return false
	}
	return validFrozenSeriesHoldout(FrozenSeriesHoldoutEvidence{
		SchemaVersion: 1, TenantID: tenantID,
		EventID: candidate.EventID, AssetVersionID: source.AssetVersionID,
		ApplyRunID: source.ApplyRunID, ItemID: source.ItemID,
		ProducerTaskID: source.ProducerTaskID,
		SourceImage:    source.SourceImage, StageImage: source.StageImage,
		Params: source.Params, Series: source.Series,
	})
}

// LoadSeriesProducerCandidate reads the canonical Apply chain in the same
// transaction that holds the training candidate snapshot. No producer ID is
// accepted from an API caller. Physical Params/Series bytes and publication
// must still be checked separately.
func LoadSeriesProducerCandidate(ctx context.Context, tx pgx.Tx,
	start TrainingStart, candidate TrainingCandidate,
	render TrainingRenderCase) (SeriesProducerCandidate, error) {
	if err := ctx.Err(); err != nil {
		return SeriesProducerCandidate{}, err
	}
	if tx == nil || start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil ||
		candidate.EventID == uuid.Nil {
		return SeriesProducerCandidate{}, ErrInvalidTrainingDraft
	}
	var source SeriesProducerCandidate
	err := tx.QueryRow(ctx, `SELECT asset_version_id,consent_id,consent_epoch,
 apply_run_id,item_id,producer_task_id,source_image_id,source_image_sha,
 stage_image_id,stage_image_sha,params_id,params_sha,series_id,series_sha
 FROM photo.v23_series_event_source($1,$2,$3,$4,$5)`,
		start.TenantID, start.ProjectID, start.ActorID,
		start.AuthSessionID, candidate.EventID).Scan(
		&source.AssetVersionID, &source.ConsentID, &source.ConsentEpoch,
		&source.ApplyRunID, &source.ItemID, &source.ProducerTaskID,
		&source.SourceImage.ArtifactID, &source.SourceImage.SHA256,
		&source.StageImage.ArtifactID, &source.StageImage.SHA256,
		&source.Params.ArtifactID, &source.Params.SHA256,
		&source.Series.ArtifactID, &source.Series.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return SeriesProducerCandidate{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return SeriesProducerCandidate{}, err
	}
	if err := ctx.Err(); err != nil {
		return SeriesProducerCandidate{}, err
	}
	if !validSeriesProducerCandidate(start.TenantID, source, candidate, render) {
		return SeriesProducerCandidate{}, ErrInvalidTrainingDraft
	}
	return source, nil
}
