package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type completedNativeInputDefect struct {
	catalog  nativeInputDefectResultCatalog
	objects  [6]defectEvidenceObject
	evidence VerifiedDefectEvidence
}

// Exact native dependency and physical result verification, outside SQL.
// A stale catalog/dependency or any invalid byte stream returns no evidence.
func (s SQLTransferSourceReader) readCompletedNativeInputDefectUncached(ctx context.Context, who auth.Identity, plan nativeDefectDraftPlan, color TransferColorProfile) (completedNativeInputDefect, error) {
	var zero completedNativeInputDefect
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	catalog, err := s.nativeInputDefectResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	p := plan.prepared.dependencies.preparation
	landmarks, err := s.readCompletedNativeLandmarks(ctx, who, p.landmarksPlan, color)
	if err != nil {
		return zero, err
	}
	skin, err := s.readCompletedNativeSkin(ctx, who, p.skinPlan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(landmarks, p.landmarks) || !reflect.DeepEqual(skin, p.skin) {
		return zero, v2input.ErrConflict
	}
	objects, err := s.nativeInputDefectResultObjects(ctx, who, plan, catalog)
	if err != nil {
		return zero, err
	}
	bodies := make([]json.RawMessage, 3)
	for i, ordinal := range []int{5, 3, 4} {
		bodies[i], err = v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[ordinal].object, objects[ordinal].ref)
		if err != nil {
			return zero, err
		}
	}
	if string(bodies[0]) != p.params.params {
		return zero, v2input.ErrConflict
	}
	binding := p.landmarksPlan.detector.request.Intent.Reference
	image := json.RawMessage(landmarks.detector.sourceBody)
	pins := DefectOriginPins{Params: catalog.Params, Proposal: catalog.Outputs[3], Report: catalog.Outputs[4], Labels: catalog.Outputs[0], Protection: catalog.Outputs[1]}
	protection, err := v2input.VerifySemanticDefectEvidence(ctx, pins.Params, bodies[0], pins.Report, bodies[2], pins.Proposal, bodies[1], binding, image)
	if err != nil {
		return zero, err
	}
	labels, err := v2input.ResolveSemanticDefectLabels(ctx, pins.Proposal, bodies[1], binding, image)
	if err != nil {
		return zero, err
	}
	if protection != pins.Protection || labels != pins.Labels {
		return zero, v2input.ErrConflict
	}
	parsed, err := review.ParseWorkingImage(image)
	if err != nil {
		return zero, err
	}
	source := parsed.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	proposalRef := review.ArtifactRef{ID: pins.Proposal.ArtifactID, SHA256: pins.Proposal.SHA256}
	masks, err := review.ProposalFaceMasks(bodies[2], proposalRef, source)
	if err != nil {
		return zero, err
	}
	png, err := review.ProposalLabelPNG(bodies[2], proposalRef, source)
	if err != nil {
		return zero, err
	}
	if png != (review.ArtifactRef{ID: catalog.Outputs[2].ArtifactID, SHA256: catalog.Outputs[2].SHA256}) {
		return zero, v2input.ErrConflict
	}
	if err := s.verifyDefectLabelPNG(ctx, objects[0], objects[2], parsed.Canvas.Width, parsed.Canvas.Height); err != nil {
		return zero, err
	}
	if err := s.verifyDefectPixels(ctx, binding, image, pins, bodies[1], objects[0], objects[1]); err != nil {
		return zero, err
	}
	declared, err := review.ProposalDeclaredProtections(bodies[2], proposalRef, source)
	if err != nil {
		return zero, err
	}
	if err := s.verifyNativeDefectPersonOutput(ctx, who, p, declared, objects[1]); err != nil {
		return zero, err
	}
	current, err := s.nativeInputDefectResultCatalogCurrent(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	currentObjects, err := s.nativeInputDefectResultObjects(ctx, who, plan, current)
	if err != nil {
		return zero, err
	}
	currentLandmarks, err := s.readCompletedNativeLandmarks(ctx, who, p.landmarksPlan, color)
	if err != nil {
		return zero, err
	}
	currentSkin, err := s.readCompletedNativeSkin(ctx, who, p.skinPlan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, catalog) || currentObjects != objects || !reflect.DeepEqual(currentLandmarks, landmarks) || !reflect.DeepEqual(currentSkin, skin) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativeDefectPersonInput(ctx, who, p); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return completedNativeInputDefect{catalog: catalog, objects: objects, evidence: VerifiedDefectEvidence{Params: bodies[0], Proposal: bodies[1], Report: bodies[2], Labels: labels, Protection: protection, LabelPNG: catalog.Outputs[2], FaceMasks: masks}}, nil
}
