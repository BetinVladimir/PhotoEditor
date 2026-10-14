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

// SQLTrainingReviewedDustPreparer discovers only the accepted Apply review
// associated with the locked training event. No review ID comes from input.
type SQLTrainingReviewedDustPreparer struct {
	DB      *pgxpool.Pool // photo_scheduler
	Catalog v2draft.ArtifactCatalog
	Objects LearningEvaluateObjectReader
	Reviews SQLReviewedDustObjects
}

func reviewedDustStyleEnabled(raw []byte) (bool, error) {
	var style struct {
		Extensions struct {
			Restoration *struct {
				SensorDust string `json:"sensor_dust"`
				Denoise    string `json:"denoise"`
				Upscale    string `json:"upscale"`
			} `json:"restoration"`
		} `json:"extensions"`
	}
	if json.Unmarshal(raw, &style) != nil {
		return false, ErrInvalidTrainingDraft
	}
	if style.Extensions.Restoration == nil {
		return false, nil
	}
	switch style.Extensions.Restoration.SensorDust {
	case "review":
		if style.Extensions.Restoration.Denoise != "off" ||
			style.Extensions.Restoration.Upscale != "off" {
			return false, ErrInvalidTrainingDraft
		}
		return true, nil
	case "off", "conservative_auto":
		return false, nil
	default:
		return false, ErrInvalidTrainingDraft
	}
}

func (p SQLTrainingReviewedDustPreparer) root(ctx context.Context,
	who auth.Identity, tenantID uuid.UUID, path, contract string,
	ref v2input.Ref) ([]byte, error) {
	object, err := p.Catalog.Lookup(ctx, who, tenantID,
		v2draft.LayerRef{Path: path, Contract: contract, Ref: ref})
	if err != nil {
		return nil, err
	}
	return ReadFrozenRootDocument(ctx, p.Objects, object, ref)
}

func (p SQLTrainingReviewedDustPreparer) Prepare(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate, runID uuid.UUID,
	render TrainingRenderCase, recipes CompiledTrainingRecipes,
	image TrainingImage, protected TrainingMask) (*PreparedReviewedDustHoldout, error) {
	if p.DB == nil || p.Catalog == nil || p.Objects == nil ||
		p.Reviews.DB == nil || p.Reviews.Objects == nil ||
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
	enabled, err := reviewedDustStyleEnabled(styleRaw)
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
	prepared, err := PrepareReviewedDustForCandidate(ctx, tx, p.Reviews,
		start, candidate, runID, render, recipes, protectedRaw)
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
		prepared.Document.Protection != render.Protected {
		return nil, ErrTrainingDraftConflict
	}
	return &prepared, nil
}
