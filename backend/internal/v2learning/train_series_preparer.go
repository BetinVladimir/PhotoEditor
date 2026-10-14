package v2learning

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// SQLTrainingSeriesPreparer discovers the original Apply series only for an
// enabled immutable StyleV2. It prepares evidence but cannot publish it.
type SQLTrainingSeriesPreparer struct {
	DB      *pgxpool.Pool // photo_scheduler
	Catalog v2draft.ArtifactCatalog
	Objects LearningEvaluateObjectReader
	Series  SQLSeriesObjects
}

func trainingSeriesStyleEnabled(raw []byte) (bool, error) {
	var style struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if json.Unmarshal(raw, &style) != nil {
		return false, ErrInvalidTrainingDraft
	}
	seriesRaw := style.Extensions["series"]
	if len(seriesRaw) == 0 || bytes.Equal(seriesRaw, []byte("null")) {
		return false, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(seriesRaw, &fields) != nil || fields == nil {
		return false, ErrInvalidTrainingDraft
	}
	var enabled *bool
	if json.Unmarshal(fields["enabled"], &enabled) != nil || enabled == nil {
		return false, ErrInvalidTrainingDraft
	}
	if !*enabled {
		if len(fields) != 1 {
			return false, ErrInvalidTrainingDraft
		}
		return false, nil
	}
	if len(fields) != 5 {
		return false, ErrInvalidTrainingDraft
	}
	var series struct {
		Algorithm     string    `json:"algorithm"`
		Reference     string    `json:"reference"`
		EVLimit       float64   `json:"ev_limit"`
		RGBGainLimits []float64 `json:"rgb_gain_limits"`
	}
	if json.Unmarshal(seriesRaw, &series) != nil ||
		series.Algorithm != "seriesmatch_v1" || series.Reference != "median" ||
		series.EVLimit != .35 ||
		len(series.RGBGainLimits) != 2 ||
		series.RGBGainLimits[0] != .95 ||
		series.RGBGainLimits[1] != 1.05 {
		return false, ErrInvalidTrainingDraft
	}
	return true, nil
}

func (p SQLTrainingSeriesPreparer) Prepare(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate, runID uuid.UUID,
	render TrainingRenderCase, recipes CompiledTrainingRecipes,
	image TrainingImage, protected TrainingMask) (*PreparedSeriesHoldout, error) {
	if p.DB == nil || p.Catalog == nil || p.Objects == nil ||
		p.Series.DB == nil || p.Series.Objects == nil ||
		runID == uuid.Nil || candidate.EventID == uuid.Nil ||
		!validManifestRef(recipes.Base.BaseStyle) ||
		!validManifestRef(render.Protected) ||
		!validManifestRef(protected.Pixels) || image.PixelsCount < 1 {
		return nil, ErrInvalidTrainingDraft
	}
	who := auth.Identity{UserID: start.ActorID, SessionID: start.AuthSessionID}
	styleRef := v2draft.LayerRef{Path: "TrainingRenderCase.base_style",
		Contract: "StyleV2", Ref: recipes.Base.BaseStyle}
	object, err := p.Catalog.Lookup(ctx, who, start.TenantID, styleRef)
	if err != nil {
		return nil, err
	}
	styleRaw, err := ReadFrozenRootDocument(ctx, p.Objects,
		object, recipes.Base.BaseStyle)
	if err != nil {
		return nil, err
	}
	enabled, err := trainingSeriesStyleEnabled(styleRaw)
	if err != nil || !enabled {
		return nil, err
	}
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	prepared, err := PrepareSeriesHoldoutForCandidate(ctx, tx, p.Series,
		start, candidate, render, runID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	objectAfter, err := p.Catalog.Lookup(ctx, who, start.TenantID, styleRef)
	if err != nil {
		return nil, err
	}
	styleAfter, err := ReadFrozenRootDocument(ctx, p.Objects,
		objectAfter, recipes.Base.BaseStyle)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(styleRaw, styleAfter) ||
		prepared.Document.SourceImage != render.WorkingImage {
		return nil, ErrTrainingDraftConflict
	}
	return &prepared, nil
}
