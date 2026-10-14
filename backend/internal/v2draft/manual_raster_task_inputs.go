package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ResolveManualRasterTaskInputEvidence binds original producer documents before
// performing scoped catalog traversal. Original Params remain an explicit root
// even when the raster document points to external vectors. This inventory is
// not live task admission or verification of binary raster contents.
func (s ClosureResolver) ResolveManualRasterTaskInputEvidence(ctx context.Context, who auth.Identity,
	tenant uuid.UUID, binding v2input.SessionVersionBinding, sourceRaw []byte,
	rootRef v2input.Ref, rootRaw []byte, paramsRef v2input.Ref, paramsRaw, vectorsRaw []byte, targetRef, guardRef v2input.Ref) (TaskInputEvidence, error) {
	return s.resolveManualRasterTaskInputEvidence(ctx, who, tenant, binding, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetRef, guardRef, nil)
}

func (s ClosureResolver) resolveManualRasterTaskInputEvidence(ctx context.Context, who auth.Identity,
	tenant uuid.UUID, binding v2input.SessionVersionBinding, sourceRaw []byte,
	rootRef v2input.Ref, rootRaw []byte, paramsRef v2input.Ref, paramsRaw, vectorsRaw []byte,
	targetRef, guardRef v2input.Ref, local *LayerRef) (TaskInputEvidence, error) {
	if err := ctx.Err(); err != nil {
		return TaskInputEvidence{}, err
	}
	if tenant == uuid.Nil || binding.AssetVersionID == uuid.Nil {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	origin, err := CompileManualRasterVectors(binding.Source, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw)
	if err != nil {
		return TaskInputEvidence{}, err
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	hash, err := canonical.Hash(source.Canvas)
	if err != nil || hash != binding.CanvasHash {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	root, err := review.ParseManualRasterDocument(rootRaw, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256})
	if err != nil {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	if err = review.VerifyManualMaskDocumentRefs(root, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256}, review.ArtifactRef{ID: targetRef.ArtifactID, SHA256: targetRef.SHA256}, review.ArtifactRef{ID: guardRef.ArtifactID, SHA256: guardRef.SHA256}); err != nil {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	roots := []LayerRef{
		{Path: "TaskInputs.source", Contract: "WorkingImage", Ref: binding.Source, CanvasHash: binding.CanvasHash},
		{Path: "TaskInputs.target", Contract: "MaskRef", Ref: targetRef, CanvasHash: binding.CanvasHash},
		{Path: "TaskInputs.guard", Contract: "MaskRef", Ref: guardRef, CanvasHash: binding.CanvasHash},
		{Path: "TaskInputs.original_params", Contract: "Params", Ref: paramsRef},
		{Path: "TaskInputs.manual_raster", Contract: "RasterizedMask", Ref: rootRef, ManualVectors: &origin},
	}
	if local != nil {
		roots = append(roots, *local)
	}
	result := TaskInputEvidence{Documents: []DocumentWitness{}, Occurrences: []OccurrenceWitness{}}
	var entries []v2input.Entry
	if _, err = s.resolveRootsWithEvidence(ctx, who, tenant, roots, binding, &entries, &result.Documents, &result.Occurrences); err != nil {
		return TaskInputEvidence{}, err
	}
	result.Entries, err = uniqueTaskInputs(entries)
	if err != nil {
		return TaskInputEvidence{}, err
	}
	return result, nil
}
