package v2learning

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// SQLTrainingReviewedDefectPreparer owns review discovery. A caller never
// supplies a review ID; SQL derives exactly one accepted repair for the event.
type SQLTrainingReviewedDefectPreparer struct {
	DB      *pgxpool.Pool // photo_scheduler
	Catalog v2draft.ArtifactCatalog
	Objects LearningEvaluateObjectReader
	Reviews SQLReviewedDefectObjects
}

func (p SQLTrainingReviewedDefectPreparer) currentSource(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate,
	render TrainingRenderCase) (ReviewedDefectSource, error) {
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return ReviewedDefectSource{}, err
	}
	defer tx.Rollback(ctx)
	source, err := LoadReviewedDefectForCandidate(ctx, tx, start,
		candidate, render)
	if err != nil {
		return ReviewedDefectSource{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReviewedDefectSource{}, err
	}
	return source, nil
}

func (p SQLTrainingReviewedDefectPreparer) root(ctx context.Context,
	who auth.Identity, tenantID uuid.UUID, path, contract string,
	ref v2input.Ref) ([]byte, error) {
	object, err := p.Catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{
		Path: path, Contract: contract, Ref: ref})
	if err != nil {
		return nil, err
	}
	return ReadFrozenRootDocument(ctx, p.Objects, object, ref)
}

func (p SQLTrainingReviewedDefectPreparer) Prepare(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate, runID uuid.UUID,
	render TrainingRenderCase, recipes CompiledTrainingRecipes,
	image TrainingImage, protected TrainingMask) (*PreparedReviewedDefectHoldout, error) {
	if p.DB == nil || p.Catalog == nil || p.Objects == nil ||
		p.Reviews.DB == nil || p.Reviews.Objects == nil ||
		runID == uuid.Nil || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || candidate.EventID == uuid.Nil ||
		!validManifestRef(recipes.Base.BaseStyle) ||
		!validManifestRef(render.Protected) ||
		!validManifestRef(protected.Pixels) ||
		image.PixelsCount < 1 {
		return nil, ErrInvalidTrainingDraft
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	who := auth.Identity{UserID: start.ActorID,
		SessionID: start.AuthSessionID}
	styleRaw, err := p.root(ctx, who, start.TenantID,
		"TrainingRenderCase.base_style", "StyleV2", recipes.Base.BaseStyle)
	if err != nil {
		return nil, err
	}
	enabled, err := reviewedDefectStyleEnabled(styleRaw)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	source, err := p.currentSource(ctx, start, candidate, render)
	if err != nil {
		return nil, err
	}
	load := func(role string, ref v2input.Ref) ([]byte, error) {
		return p.Reviews.LoadJSON(ctx, start.TenantID,
			start.ProjectID, start.ActorID, start.AuthSessionID,
			source.Decision.ReviewID, role, ref)
	}
	imageRaw, err := load("image", source.Image)
	if err != nil {
		return nil, err
	}
	proposalRaw, err := load("proposal", source.Proposal)
	if err != nil {
		return nil, err
	}
	selectionRaw, err := load("selection", source.Selection)
	if err != nil {
		return nil, err
	}
	resolvedRaw, err := load("resolved", source.Resolved)
	if err != nil {
		return nil, err
	}
	paramsRaw, err := load("params", source.Params)
	if err != nil {
		return nil, err
	}
	selection, err := review.ParseDefectSelection(selectionRaw,
		review.ArtifactRef{ID: source.Selection.ArtifactID,
			SHA256: source.Selection.SHA256})
	if err != nil || !reflect.DeepEqual(selection,
		source.Decision.Selection) {
		return nil, ErrInvalidTrainingDraft
	}
	proposal, err := review.ParseDefectProposalStructure(ctx, proposalRaw,
		review.ArtifactRef{ID: source.Proposal.ArtifactID,
			SHA256: source.Proposal.SHA256})
	if err != nil {
		return nil, err
	}
	bound, err := proposal.Bind(review.DefectSnapshot{
		Proposal: review.ArtifactRef{ID: source.Proposal.ArtifactID,
			SHA256: source.Proposal.SHA256},
		Image: review.ArtifactRef{ID: source.Image.ArtifactID,
			SHA256: source.Image.SHA256},
		LabelMap: review.ArtifactRef{ID: source.LabelMap.ArtifactID,
			SHA256: source.LabelMap.SHA256}})
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(bound.CandidateIDs))
	for _, id := range bound.CandidateIDs {
		known[id] = true
	}
	for _, group := range [][]string{selection.Accepted, selection.Rejected} {
		for _, id := range group {
			if !known[id] {
				return nil, ErrInvalidTrainingDraft
			}
		}
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
	prepared, err := PrepareReviewedDefectHoldout(candidate, runID,
		render, recipes, source, styleRaw, resolvedRaw, paramsRaw,
		imageRaw, protectedRaw)
	if err != nil || prepared.ProtectionPixels != protected.Pixels {
		return nil, ErrInvalidTrainingDraft
	}
	before, err := p.Reviews.AuthorizeProtection(ctx, start.TenantID,
		start.ProjectID, start.ActorID, start.AuthSessionID,
		source.Decision.ReviewID, protected.Pixels)
	if err != nil {
		return nil, err
	}
	current, err := p.currentSource(ctx, start, candidate, render)
	if err != nil {
		return nil, err
	}
	after, err := p.Reviews.AuthorizeProtection(ctx, start.TenantID,
		start.ProjectID, start.ActorID, start.AuthSessionID,
		source.Decision.ReviewID, protected.Pixels)
	if err != nil {
		return nil, err
	}
	if before != after || !reflect.DeepEqual(source, current) {
		return nil, ErrTrainingDraftConflict
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
		!bytes.Equal(protectedRaw, maskAfter) {
		return nil, ErrTrainingDraftConflict
	}
	return &prepared, nil
}

func reviewedDefectStyleEnabled(styleRaw []byte) (bool, error) {
	var style struct {
		Defects struct {
			Enabled *bool `json:"enabled"`
		} `json:"defects"`
	}
	if json.Unmarshal(styleRaw, &style) != nil ||
		style.Defects.Enabled == nil {
		return false, ErrInvalidTrainingDraft
	}
	return *style.Defects.Enabled, nil
}
