package v2learning

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PreparedSeriesHoldout contains checked value bytes. It has no publication
// or Evaluate authority until the training task input closure is registered.
type PreparedSeriesHoldout struct {
	Document   FrozenSeriesHoldoutEvidence
	Body       []byte
	SHA256     string
	Correction FrozenSeriesItemCorrection
}

// PrepareSeriesHoldoutForCandidate rechecks the unique event source on both
// sides of physical IO. SQLSeriesObjects repeats its own exact-version lookup
// around each object read. The source image is read so a stale master cannot
// be represented by valid metadata alone.
func PrepareSeriesHoldoutForCandidate(ctx context.Context, tx pgx.Tx,
	objects SQLSeriesObjects, start TrainingStart,
	candidate TrainingCandidate, render TrainingRenderCase,
	trainingRunID uuid.UUID) (PreparedSeriesHoldout, error) {
	if trainingRunID == uuid.Nil || objects.DB == nil || objects.Objects == nil {
		return PreparedSeriesHoldout{}, ErrInvalidTrainingDraft
	}
	before, err := LoadSeriesProducerCandidate(ctx, tx, start, candidate, render)
	if err != nil {
		return PreparedSeriesHoldout{}, err
	}
	if _, err = objects.LoadJSON(ctx, start, candidate.EventID,
		"source_image", before.SourceImage); err != nil {
		return PreparedSeriesHoldout{}, err
	}
	if _, err = objects.LoadJSON(ctx, start, candidate.EventID,
		"stage_image", before.StageImage); err != nil {
		return PreparedSeriesHoldout{}, err
	}
	paramsRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"params", before.Params)
	if err != nil {
		return PreparedSeriesHoldout{}, err
	}
	seriesRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"series", before.Series)
	if err != nil {
		return PreparedSeriesHoldout{}, err
	}
	after, err := LoadSeriesProducerCandidate(ctx, tx, start, candidate, render)
	if err != nil {
		return PreparedSeriesHoldout{}, err
	}
	if before != after {
		return PreparedSeriesHoldout{}, ErrTrainingDraftConflict
	}
	document := FrozenSeriesHoldoutEvidence{
		SchemaVersion: 1, TenantID: start.TenantID,
		EventID: candidate.EventID, AssetVersionID: before.AssetVersionID,
		ApplyRunID: before.ApplyRunID, ItemID: before.ItemID,
		ProducerTaskID: before.ProducerTaskID,
		SourceImage:    before.SourceImage, StageImage: before.StageImage,
		Params: before.Params, Series: before.Series,
	}
	selected, err := ValidateFrozenSeriesMaterial(ctx, document,
		paramsRaw, seriesRaw)
	if err != nil {
		return PreparedSeriesHoldout{}, err
	}
	body, sha, err := CompileFrozenSeriesHoldoutEvidence(document)
	if err != nil {
		return PreparedSeriesHoldout{}, err
	}
	if _, err = SeriesHoldoutRef(trainingRunID, candidate.EventID, sha); err != nil {
		return PreparedSeriesHoldout{}, err
	}
	return PreparedSeriesHoldout{Document: document, Body: body,
		SHA256: sha, Correction: selected}, nil
}
