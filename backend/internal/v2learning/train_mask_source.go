package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingMask struct {
	Document review.MaskRef
	Pixels   v2input.Ref
}

type TrainingMaskSource struct {
	Catalog    v2draft.ArtifactCatalog
	Objects    LearningEvaluateObjectReader
	Provenance TrainingMaskProvenanceVerifier
}

type TrainingMaskProvenanceVerifier interface {
	Verify(context.Context, TrainingStart, TrainingCandidate,
		v2input.Ref, v2input.Ref, bool) error
}

// Load validates an already selected MaskRef against the frozen image. Mask
// selection must be backed by event/producer provenance before calling this.
func (s TrainingMaskSource) Load(ctx context.Context, who auth.Identity,
	start TrainingStart, candidate TrainingCandidate,
	ref v2input.Ref, image TrainingImage,
	protected bool) (TrainingMask, error) {
	var empty TrainingMask
	if s.Catalog == nil || s.Objects == nil || s.Provenance == nil ||
		start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		candidate.EventID == uuid.Nil || candidate.AssetVersionID == uuid.Nil ||
		!validManifestRef(ref) || image.PixelsCount < 1 {
		return empty, ErrInvalidTrainingDraft
	}
	root, err := s.Catalog.Lookup(ctx, who, start.TenantID, v2draft.LayerRef{
		Path: "TrainingRenderCase.mask", Contract: "MaskRef", Ref: ref})
	if err != nil {
		return empty, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, root, ref)
	if err != nil {
		return empty, err
	}
	var pixels v2input.Ref
	if protected {
		pixels, err = ParseFrozenProtectedMask(body, image.Document)
	} else {
		pixels, err = ParseFrozenEffectiveMask(body, image.Document)
	}
	if err != nil {
		return empty, err
	}
	mask, err := review.ParseMaskRef(body)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	if err := s.Provenance.Verify(ctx, start, candidate, ref, pixels,
		protected); err != nil {
		return empty, err
	}
	child, err := s.Catalog.Lookup(ctx, who, start.TenantID,
		v2draft.LayerRef{Path: "TrainingRenderCase.mask.artifact",
			Contract: "MaskPixels", Ref: pixels, ParentRef: ref})
	if err != nil {
		return empty, err
	}
	if child.SHA256 != pixels.SHA256 || child.Bytes < 32 ||
		child.ContentType != "application/octet-stream" ||
		child.VersionID == "" || child.VersionID == "null" {
		return empty, ErrInvalidTrainingDraft
	}
	return TrainingMask{Document: mask, Pixels: pixels}, nil
}
