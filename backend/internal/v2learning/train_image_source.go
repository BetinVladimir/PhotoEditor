package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingImage struct {
	Document    review.WorkingImage
	Pixels      v2input.Ref
	Alpha       *v2input.Ref
	PixelsCount int64
}

type TrainingImageSource struct {
	Catalog v2draft.ArtifactCatalog
	Objects LearningEvaluateObjectReader
}

// Load pins the WorkingImage root and its binary children through the same
// project/version/producer scoped catalog used by immutable draft admission.
func (s TrainingImageSource) Load(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, binding v2input.SessionVersionBinding,
	iccLock string) (TrainingImage, error) {
	if !validSHA(iccLock) {
		return TrainingImage{}, ErrInvalidTrainingDraft
	}
	return s.load(ctx, who, tenantID, binding, iccLock)
}

// DiscoverICC reads the same scoped, byte-verified WorkingImage closure as
// Load, before a new run has chosen its one ICC lock. Resolve must then pass
// the discovered SHA to Load for every selected training case.
func (s TrainingImageSource) DiscoverICC(ctx context.Context,
	who auth.Identity, tenantID uuid.UUID,
	binding v2input.SessionVersionBinding) (string, error) {
	image, err := s.load(ctx, who, tenantID, binding, "")
	if err != nil {
		return "", err
	}
	return image.Document.Color.ICCSHA256, nil
}

func (s TrainingImageSource) load(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, binding v2input.SessionVersionBinding,
	iccLock string) (TrainingImage, error) {
	var empty TrainingImage
	if s.Catalog == nil || s.Objects == nil || tenantID == uuid.Nil ||
		binding.AssetVersionID == uuid.Nil ||
		!validManifestRef(binding.Source) || !validSHA(binding.CanvasHash) ||
		(iccLock != "" && !validSHA(iccLock)) {
		return empty, ErrInvalidTrainingDraft
	}
	root, err := s.Catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{
		Path: "EditingRecipeV3.source.source", Contract: "WorkingImage",
		Ref: binding.Source, CanvasHash: binding.CanvasHash})
	if err != nil {
		return empty, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, root, binding.Source)
	if err != nil {
		return empty, err
	}
	image, err := ParseFrozenEvaluationImage(body, binding)
	if err != nil || !validSHA(image.Color.ICCSHA256) ||
		(iccLock != "" && image.Color.ICCSHA256 != iccLock) {
		return empty, ErrInvalidTrainingDraft
	}
	child := func(path, contract string, ref v2input.Ref) error {
		if !validManifestRef(ref) {
			return ErrInvalidTrainingDraft
		}
		object, err := s.Catalog.Lookup(ctx, who, tenantID,
			v2draft.LayerRef{Path: path, Contract: contract, Ref: ref,
				ParentRef: binding.Source})
		if err != nil {
			return err
		}
		if object.ContentType != "application/octet-stream" ||
			object.SHA256 != ref.SHA256 || object.Bytes < 1 ||
			object.VersionID == "" || object.VersionID == "null" {
			return ErrInvalidTrainingDraft
		}
		return nil
	}
	pixels := v2input.Ref{ArtifactID: image.Pixels.ID,
		SHA256: image.Pixels.SHA256}
	if err := child("WorkingImage.pixels", "WorkingPixels", pixels); err != nil {
		return empty, err
	}
	var alpha *v2input.Ref
	if image.Alpha != nil {
		ref := v2input.Ref{ArtifactID: image.Alpha.ID,
			SHA256: image.Alpha.SHA256}
		if err := child("WorkingImage.alpha", "WorkingAlpha", ref); err != nil {
			return empty, err
		}
		alpha = &ref
	}
	return TrainingImage{Document: image, Pixels: pixels, Alpha: alpha,
		PixelsCount: image.Canvas.Width * image.Canvas.Height}, nil
}
