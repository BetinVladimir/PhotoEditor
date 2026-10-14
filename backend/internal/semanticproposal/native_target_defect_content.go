package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type completedNativeTargetDefect struct {
	catalog  nativeInputDefectResultCatalog
	objects  [6]defectEvidenceObject
	evidence VerifiedDefectEvidence
}

// Exact native dependency and physical result verification, outside SQL.
// A stale catalog/dependency or any invalid byte stream returns no evidence.
func (s SQLTransferSourceReader) readCompletedNativeTargetDefect(ctx context.Context, who auth.Identity, plan nativeTargetDefectDraftPlan, color TransferColorProfile) (completedNativeTargetDefect, error) {
	var zero completedNativeTargetDefect
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	catalog, err := s.nativeTargetDefectResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	p := plan.prepared.preparation
	if err := s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return zero, err
	}
	if err := s.recheckNativeTargetDefectQualification(ctx, who, p, plan.prepared.qualification, color); err != nil {
		return zero, err
	}
	landmarks := p.landmarks
	objects, err := s.nativeTargetDefectResultObjects(ctx, who, plan, catalog)
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
	binding := plan.anchor.entry.Binding
	image := json.RawMessage(landmarks.detector.completed.sourceBody)
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
	if err := s.verifyNativeTargetDefectPersonOutput(ctx, plan, declared, objects[1]); err != nil {
		return zero, err
	}
	current, err := s.nativeTargetDefectResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	currentObjects, err := s.nativeTargetDefectResultObjects(ctx, who, plan, current)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, catalog) || currentObjects != objects {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return zero, err
	}
	if err := s.recheckNativeTargetDefectQualification(ctx, who, p, plan.prepared.qualification, color); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return completedNativeTargetDefect{catalog: catalog, objects: objects, evidence: VerifiedDefectEvidence{Params: bodies[0], Proposal: bodies[1], Report: bodies[2], Labels: labels, Protection: protection, LabelPNG: catalog.Outputs[2], FaceMasks: masks}}, nil
}
