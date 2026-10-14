package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ReadManualPreparation verifies original source-bound manual documents and all
// four binary outputs before a defect producer exists. It compares both NPY streams with independent vector reconstruction; broader
// numeric qualification and retained final admission remain separate obligations.
func (s SQLTransferSourceReader) ReadManualPreparation(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, target review.ProtectionMask) (review.OriginalManualProtection, error) {
	original, _, err := s.readManualPreparationProof(ctx, who, tenantID, binding, target)
	return original, err
}

func (s SQLTransferSourceReader) readManualPreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, target review.ProtectionMask) (review.OriginalManualProtection, manualPreparationProof, error) {
	s = s.withManualRasterScope()
	empty := review.OriginalManualProtection{}
	if err := ctx.Err(); err != nil {
		return empty, manualPreparationProof{}, err
	}
	targetBody, err := canonical.Marshal(target)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	if err = json.Unmarshal(targetBody, &target); err != nil {
		return empty, manualPreparationProof{}, err
	}
	sourceBody, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	image, err := review.ParseWorkingImage(sourceBody)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	pixel := v2input.Ref{ArtifactID: target.Artifact.ID, SHA256: target.Artifact.SHA256}
	catalog, err := s.manualPreparationObjects(ctx, who, tenantID, binding, pixel)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	rootBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, catalog.Root.object, catalog.Root.ref)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	paramsBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, catalog.Params.object, catalog.Params.ref)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	ref := func(r v2input.Ref) review.ArtifactRef { return review.ArtifactRef{ID: r.ArtifactID, SHA256: r.SHA256} }
	external, err := review.VerifyManualMaskParams(paramsBody, ref(catalog.Params.ref), rootBody, ref(catalog.Root.ref), target, source)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	var vectors *defectEvidenceObject
	var externalBody []byte
	if external != nil {
		pin, err := s.manualPreparationVectorObject(ctx, who, tenantID, binding, pixel, v2input.Ref{ArtifactID: external.ID, SHA256: external.SHA256})
		if err != nil {
			return empty, manualPreparationProof{}, err
		}
		body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return empty, manualPreparationProof{}, err
		}
		if err = review.VerifyManualVectorsDocument(body, *external, source); err != nil {
			return empty, manualPreparationProof{}, err
		}
		vectors = &pin
		externalBody = body
	}
	raster, err := review.ResolveManualRasterPlan(ctx, paramsBody, ref(catalog.Params.ref), rootBody, ref(catalog.Root.ref), target, source, externalBody)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	if err := s.reserveManualRasterBudget(raster); err != nil {
		return empty, manualPreparationProof{}, err
	}
	inventory, err := review.ResolveManualFaceMaskOutputs(rootBody, ref(catalog.Root.ref), target, source)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	childRef := func(r review.ArtifactRef) v2input.Ref { return v2input.Ref{ArtifactID: r.ID, SHA256: r.SHA256} }
	childRefs := [3]v2input.Ref{childRef(inventory.Protected.Artifact), childRef(inventory.TargetPNG), childRef(inventory.ProtectedPNG)}
	children, err := s.manualPreparationOutputObjects(ctx, who, tenantID, binding, pixel, childRefs)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	if err = s.verifyProbabilityPNG(ctx, catalog.Pixel, children[1], image.Canvas.Width, image.Canvas.Height); err != nil {
		return empty, manualPreparationProof{}, err
	}
	if err = s.verifyProbabilityPNG(ctx, children[0], children[2], image.Canvas.Width, image.Canvas.Height); err != nil {
		return empty, manualPreparationProof{}, err
	}
	if err := s.verifyManualRaster(ctx, catalog.Pixel, children[0], raster); err != nil {
		return empty, manualPreparationProof{}, err
	}
	current, err := s.manualPreparationObjects(ctx, who, tenantID, binding, pixel)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	if current != catalog {
		return empty, manualPreparationProof{}, v2input.ErrConflict
	}
	currentChildren, err := s.manualPreparationOutputObjects(ctx, who, tenantID, binding, pixel, childRefs)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	if currentChildren != children {
		return empty, manualPreparationProof{}, v2input.ErrConflict
	}
	if vectors != nil {
		currentVector, err := s.manualPreparationVectorObject(ctx, who, tenantID, binding, pixel, vectors.ref)
		if err != nil {
			return empty, manualPreparationProof{}, err
		}
		if currentVector != *vectors {
			return empty, manualPreparationProof{}, v2input.ErrConflict
		}
	}
	currentSource, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, manualPreparationProof{}, err
	}
	if !bytes.Equal(currentSource, sourceBody) {
		return empty, manualPreparationProof{}, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return empty, manualPreparationProof{}, err
	}
	return review.OriginalManualProtection{Root: ref(catalog.Root.ref), Body: rootBody, Target: inventory.Target, Raster: raster}, manualPreparationProof{tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, binding: binding, objects: catalog, children: children, vectors: vectors}, nil
}
