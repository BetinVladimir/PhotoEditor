package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Exact native dependency and physical result verification, outside SQL.
// A stale catalog/dependency or any invalid byte stream returns no evidence.
func (s SQLTransferSourceReader) readCompletedNativeInputManualInitialDefect(ctx context.Context, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile) (completedNativeInputDefect, error) {
	var zero completedNativeInputDefect
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	catalog, err := s.nativeInputManualInitialResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	stage := plan.prepared.stage
	deps := stage.dependencies.dependencies
	p := deps.selection
	recheck := func() error {
		q := stage.qualification.qualification
		current, err := s.prepareNativeInputManualInitialQualification(ctx, who, stage.dependencies, color, q.report.report.ref, q.costs)
		if err != nil {
			return err
		}
		if !sameNativeInputManualInitialStage(ctx, current, stage) {
			return v2input.ErrConflict
		}
		return ctx.Err()
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	landmarks, err := s.readCompletedNativeLandmarks(ctx, who, deps.producers.landmarks, color)
	if err != nil {
		return zero, err
	}
	skin, err := s.readCompletedNativeSkin(ctx, who, deps.producers.skin, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(landmarks, p.landmarks) || !reflect.DeepEqual(skin, p.skin) {
		return zero, v2input.ErrConflict
	}
	objects, err := s.nativeInputManualInitialResultObjects(ctx, who, plan, catalog)
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
	binding := deps.producers.landmarks.detector.request.Intent.Reference
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
	required, err := nativeInputManualDeclaredProtectionPins(ctx, p, declared)
	if err != nil {
		return zero, err
	}
	for _, child := range required {
		if err := s.verifyManualProtectionCoverage(ctx, child, objects[1], parsed.Canvas.Width, parsed.Canvas.Height); err != nil {
			return zero, err
		}
	}
	current, err := s.nativeInputManualInitialResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	currentObjects, err := s.nativeInputManualInitialResultObjects(ctx, who, plan, current)
	if err != nil {
		return zero, err
	}
	currentLandmarks, err := s.readCompletedNativeLandmarks(ctx, who, deps.producers.landmarks, color)
	if err != nil {
		return zero, err
	}
	currentSkin, err := s.readCompletedNativeSkin(ctx, who, deps.producers.skin, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, catalog) || currentObjects != objects || !reflect.DeepEqual(currentLandmarks, landmarks) || !reflect.DeepEqual(currentSkin, skin) {
		return zero, v2input.ErrConflict
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	return completedNativeInputDefect{catalog: catalog, objects: objects, evidence: VerifiedDefectEvidence{Params: bodies[0], Proposal: bodies[1], Report: bodies[2], Labels: labels, Protection: protection, LabelPNG: catalog.Outputs[2], FaceMasks: masks}}, nil
}
