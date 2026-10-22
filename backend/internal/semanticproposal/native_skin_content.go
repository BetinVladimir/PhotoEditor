package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type completedNativeSkin struct {
	catalog          nativeSkinResultCatalog
	objects          [4]defectEvidenceObject
	parser           completedNativeFaceParse
	mask             review.ProtectionMask
	body, paramsBody string
	skinPixels       int64
}

// Byte-verified source/parser/skin handoff. SQL admission and scientific model
// acceptance are separate; stale catalog or object evidence exposes zero proof.
func (s SQLTransferSourceReader) readCompletedNativeSkinUncached(ctx context.Context, who auth.Identity, plan nativeSkinDraftPlan, color TransferColorProfile) (completedNativeSkin, error) {
	var zero completedNativeSkin
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	catalog, err := s.nativeSkinResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	parser, err := s.readCompletedNativeFaceParse(ctx, who, plan.parser, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(parser, plan.prepared.params.parser) {
		return zero, v2input.ErrConflict
	}
	objects, err := s.nativeSkinResultObjects(ctx, who, plan, catalog)
	if err != nil {
		return zero, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[2].object, objects[2].ref)
	if err != nil {
		return zero, err
	}
	params, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[3].object, objects[3].ref)
	if err != nil {
		return zero, err
	}
	root, err := review.ParseMaskRef(body)
	if err != nil {
		return zero, err
	}
	if root.Artifact != (review.ArtifactRef{ID: catalog.Outputs[0].ArtifactID, SHA256: catalog.Outputs[0].SHA256}) {
		return zero, v2input.ErrConflict
	}
	image, err := review.ParseWorkingImage([]byte(parser.detector.sourceBody))
	if err != nil {
		return zero, err
	}
	binding := plan.parser.detector.request.Intent.Reference
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	labels, err := s.faceParsePreparationPixels(ctx, who, plan.shell.Tenant, binding, parser.catalog.Outputs[1], parser.detector.catalog.Output, parser.catalog.Outputs[0])
	if err != nil {
		return zero, err
	}
	mask := review.ProtectionMask(root)
	count, err := s.verifyNativeSkinDerivation(ctx, catalog.Params, params, parser.catalog.Outputs[1], []byte(parser.body), mask, source, labels, objects[0], objects[1])
	if err != nil {
		return zero, err
	}
	current, err := s.nativeSkinResultCatalogCurrent(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, catalog) {
		return zero, v2input.ErrConflict
	}
	currentObjects, err := s.nativeSkinResultObjects(ctx, who, plan, current)
	if err != nil {
		return zero, err
	}
	currentParser, err := s.readCompletedNativeFaceParse(ctx, who, plan.parser, color)
	if err != nil {
		return zero, err
	}
	currentLabels, err := s.faceParsePreparationPixels(ctx, who, plan.shell.Tenant, binding, parser.catalog.Outputs[1], parser.detector.catalog.Output, labels.ref)
	if err != nil {
		return zero, err
	}
	if currentObjects != objects || currentLabels != labels || !reflect.DeepEqual(currentParser, parser) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return completedNativeSkin{catalog: catalog, objects: objects, parser: parser, mask: mask, body: string(body), paramsBody: string(params), skinPixels: count}, nil
}
