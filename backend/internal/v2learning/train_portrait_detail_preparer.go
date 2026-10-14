package v2learning

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// SQLTrainingPortraitDetailPreparer requires an enabled immutable StyleV2 and
// the event-linked Apply v4 producer. It does not infer origin from a SHA.
type SQLTrainingPortraitDetailPreparer struct {
	DB       *pgxpool.Pool // photo_scheduler
	Catalog  v2draft.ArtifactCatalog
	Objects  LearningEvaluateObjectReader
	Producer SQLPortraitDetailObjects
	Masks    SQLPortraitDetailMaskObjects
}

func portraitDetailStyleEnabled(raw []byte) (bool, error) {
	var style struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if json.Unmarshal(raw, &style) != nil || style.Extensions == nil {
		return false, ErrInvalidTrainingDraft
	}
	return portraitDetailModuleEnabled(style.Extensions["portrait_detail"])
}

func portraitDetailModuleEnabled(moduleRaw []byte) (bool, error) {
	if len(moduleRaw) == 0 || bytes.Equal(moduleRaw, []byte("null")) {
		return false, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(moduleRaw, &fields) != nil || fields == nil {
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
	for _, key := range []string{"teeth_strength", "shine_strength", "under_eye_strength"} {
		var strength *float64
		if json.Unmarshal(fields[key], &strength) != nil ||
			strength == nil || *strength < 0 || *strength > 1 {
			return false, ErrInvalidTrainingDraft
		}
	}
	var glassesPolicy string
	if json.Unmarshal(fields["glasses_policy"], &glassesPolicy) != nil ||
		glassesPolicy != "review" {
		return false, ErrInvalidTrainingDraft
	}
	return true, nil
}

func (p SQLTrainingPortraitDetailPreparer) root(ctx context.Context,
	who auth.Identity, tenantID uuid.UUID, path, contract string,
	ref v2input.Ref) ([]byte, error) {
	object, err := p.Catalog.Lookup(ctx, who, tenantID,
		v2draft.LayerRef{Path: path, Contract: contract, Ref: ref})
	if err != nil {
		return nil, err
	}
	return ReadFrozenRootDocument(ctx, p.Objects, object, ref)
}

func (p SQLTrainingPortraitDetailPreparer) Prepare(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate, runID uuid.UUID,
	render TrainingRenderCase, recipes CompiledTrainingRecipes,
	image TrainingImage, protected TrainingMask) (*PreparedPortraitDetailHoldout, error) {
	if p.DB == nil || p.Catalog == nil || p.Objects == nil ||
		p.Producer.DB == nil || p.Producer.Objects == nil ||
		p.Masks.DB == nil || p.Masks.Objects == nil ||
		runID == uuid.Nil || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || candidate.EventID == uuid.Nil ||
		!validManifestRef(recipes.Base.BaseStyle) ||
		!validManifestRef(render.Protected) ||
		!validManifestRef(protected.Pixels) || image.PixelsCount < 1 {
		return nil, ErrInvalidTrainingDraft
	}
	who := auth.Identity{UserID: start.ActorID, SessionID: start.AuthSessionID}
	styleRaw, err := p.root(ctx, who, start.TenantID,
		"TrainingRenderCase.base_style", "StyleV2", recipes.Base.BaseStyle)
	if err != nil {
		return nil, err
	}
	enabled, err := portraitDetailStyleEnabled(styleRaw)
	if err != nil || !enabled {
		return nil, err
	}
	protectedRaw, err := p.root(ctx, who, start.TenantID,
		"TrainingRenderCase.mask", "MaskRef", render.Protected)
	if err != nil {
		return nil, err
	}
	canonicalMask, err := canonical.Marshal(protected.Document)
	if err != nil || !bytes.Equal(canonicalMask, protectedRaw) {
		return nil, ErrInvalidTrainingDraft
	}
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	prepared, err := PreparePortraitDetailForCandidate(ctx, tx, p.Producer,
		p.Masks, start, candidate, render, protectedRaw)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	styleAfter, err := p.root(ctx, who, start.TenantID,
		"TrainingRenderCase.base_style", "StyleV2", recipes.Base.BaseStyle)
	if err != nil {
		return nil, err
	}
	maskAfter, err := p.root(ctx, who, start.TenantID,
		"TrainingRenderCase.mask", "MaskRef", render.Protected)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(styleRaw, styleAfter) ||
		!bytes.Equal(protectedRaw, maskAfter) ||
		prepared.Document.Protection != render.Protected ||
		prepared.Document.Source != render.WorkingImage {
		return nil, ErrTrainingDraftConflict
	}
	return &prepared, nil
}
