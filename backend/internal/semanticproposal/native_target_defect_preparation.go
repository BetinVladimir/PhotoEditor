package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Target-only retained physical dependencies. This is not SQL admission.
type nativeTargetDefectPreparation struct {
	landmarksPlan nativeTargetFaceDraftPlan
	skinPlan      nativeTargetSkinDraftPlan
	personPlan    nativeTargetPersonPlan
	landmarks     nativeTargetFaceResult
	skin          completedNativeTargetSkin
	person        nativeTargetPersonResult
	diameter      [2]float64
	params        PreparedDefectParams
	seal          string
}

// Qualified person pixels remain unchanged; only the permanent declaration is
// projected. No source-reference authority or replacement MaskRef is created.
func projectNativeTargetPersonProtection(ctx context.Context, plan nativeTargetPersonPlan, result nativeTargetPersonResult, image json.RawMessage) (review.ProtectionMask, error) {
	var zero review.ProtectionMask
	if err := verifyNativeTargetPersonPlan(ctx, plan); err != nil {
		return zero, err
	}
	digest, err := targetPersonResultDigest(result)
	if err != nil || result.seal == "" || digest != result.seal || result.planSeal != plan.seal || result.result == uuid.Nil || len(result.outputs) != 4 || result.outputs[0] != result.mask.Pixels || result.outputs[2] != result.mask.Root {
		return zero, v2input.ErrConflict
	}
	mask, err := review.ParseMaskRef(result.mask.Body)
	if err != nil || mask.ModelLockID == nil {
		return zero, v2input.ErrInvalid
	}
	selected, err := selectNativeQualificationPolicies(plan.person.policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	if *mask.ModelLockID != selected["mask.person"].ModelLockSHA {
		return zero, v2input.ErrConflict
	}
	pixels, err := v2input.ResolveSemanticPersonMaskPixels(result.mask.Root, result.mask.Body, plan.anchor.entry.Binding, image, *mask.ModelLockID)
	if err != nil {
		return zero, err
	}
	if pixels != result.mask.Pixels {
		return zero, v2input.ErrConflict
	}
	model := *mask.ModelLockID
	return review.ProtectionMask{Artifact: mask.Artifact, Canvas: mask.Canvas, TransformChain: append(json.RawMessage(nil), mask.TransformChain...), Selector: mask.Selector, Confidence: mask.Confidence, ModelLockID: &model, Protected: true}, ctx.Err()
}

func compileNativeTargetDefectParams(ctx context.Context, p nativeTargetDefectPreparation) (PreparedDefectParams, error) {
	var zero PreparedDefectParams
	if err := verifyNativeTargetFacePlanSeal(ctx, p.landmarksPlan); err != nil {
		return zero, err
	}
	if err := verifyNativeTargetSkinPlanSeal(ctx, p.skinPlan); err != nil {
		return zero, err
	}
	if err := verifyNativeTargetPersonPlan(ctx, p.personPlan); err != nil {
		return zero, err
	}
	a := p.landmarksPlan.detector.anchor
	b := p.skinPlan.parser.detector.anchor
	c := p.personPlan.anchor
	if p.landmarksPlan.prepared.operation != "face.landmarks" || a.seal != b.seal || a.entry.Namespace != c.entry.Namespace || a.manifest.sha != c.manifest.sha || !reflect.DeepEqual(a.entry.Binding, c.entry.Binding) || !reflect.DeepEqual(p.landmarksPlan.detector.detector.policy, p.personPlan.person.policy) || p.landmarks.planSeal != p.landmarksPlan.seal || p.landmarks.operation != "face.landmarks" || len(p.landmarks.catalog.Outputs) != 1 || p.skin.parser.planSeal != p.skinPlan.parser.seal || p.skin.parser.detector.seal != p.landmarks.detector.seal {
		return zero, v2input.ErrConflict
	}
	digest, err := targetFaceResultDigest(p.landmarks)
	if err != nil || p.landmarks.seal == "" || digest != p.landmarks.seal {
		return zero, v2input.ErrConflict
	}
	permanent, err := projectNativeTargetPersonProtection(ctx, p.personPlan, p.person, json.RawMessage(p.landmarks.detector.completed.sourceBody))
	if err != nil {
		return zero, err
	}
	faces, err := bindNativeDefectFaceInputs(ctx, p.landmarks.landmarks, review.ArtifactRef{ID: p.landmarks.catalog.Outputs[0].ArtifactID, SHA256: p.landmarks.catalog.Outputs[0].SHA256}, p.skin.mask, p.skin.parser.labels.Image)
	if err != nil {
		return zero, err
	}
	source := a.entry.Binding.Source
	return PrepareDefectParams(ctx, review.ArtifactRef{ID: source.ArtifactID, SHA256: source.SHA256}, []byte(p.landmarks.detector.completed.sourceBody), faces, p.diameter, &permanent, nil, nil)
}

func nativeTargetDefectPreparationDigest(p nativeTargetDefectPreparation) (string, error) {
	objects := make([]any, len(p.skin.objects))
	for i, o := range p.skin.objects {
		objects[i] = map[string]any{"ref": o.ref, "object": o.object}
	}
	return canonical.Hash(map[string]any{"landmarks_plan": p.landmarksPlan.seal, "skin_plan": p.skinPlan.seal, "person_plan": p.personPlan.seal, "landmarks": p.landmarks.seal, "person": p.person.seal, "skin_catalog": p.skin.catalog, "skin_objects": objects, "skin_parser": p.skin.parser.seal, "skin_mask": p.skin.mask, "skin_body": p.skin.body, "skin_params": p.skin.paramsBody, "skin_pixels": p.skin.skinPixels, "diameter": p.diameter, "params": p.params.params, "digest": p.params.digest, "inputs": p.params.inputs, "roots": p.params.roots})
}

func verifyNativeTargetDefectPreparation(ctx context.Context, p nativeTargetDefectPreparation) error {
	params, err := compileNativeTargetDefectParams(ctx, p)
	if err != nil {
		return err
	}
	if params != p.params {
		return v2input.ErrConflict
	}
	digest, err := nativeTargetDefectPreparationDigest(p)
	if err != nil {
		return err
	}
	if p.seal == "" || digest != p.seal {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Reads and repeats exact canonical target producers before handing off Params.
// Defect qualification, protected coverage and SQL locks remain mandatory.
func (s SQLTransferSourceReader) prepareNativeTargetDefectParams(ctx context.Context, who auth.Identity, landmarks nativeTargetFaceDraftPlan, skin nativeTargetSkinDraftPlan, person nativeTargetPersonPlan, color TransferColorProfile, diameter [2]float64) (nativeTargetDefectPreparation, error) {
	var zero nativeTargetDefectPreparation
	p := nativeTargetDefectPreparation{landmarksPlan: landmarks, skinPlan: skin, personPlan: person, diameter: diameter}
	var err error
	p.landmarks, err = s.readNativeTargetFaceResult(ctx, who, landmarks, color)
	if err != nil {
		return zero, err
	}
	p.skin, err = s.readCompletedNativeTargetSkin(ctx, who, skin, color)
	if err != nil {
		return zero, err
	}
	p.person, err = s.readNativeTargetPersonResult(ctx, who, person)
	if err != nil {
		return zero, err
	}
	p.params, err = compileNativeTargetDefectParams(ctx, p)
	if err != nil {
		return zero, err
	}
	p.seal, err = nativeTargetDefectPreparationDigest(p)
	if err != nil {
		return zero, err
	}
	if err = s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return zero, err
	}
	return p, ctx.Err()
}

func (s SQLTransferSourceReader) recheckNativeTargetDefectPreparation(ctx context.Context, who auth.Identity, p nativeTargetDefectPreparation, color TransferColorProfile) error {
	if err := verifyNativeTargetDefectPreparation(ctx, p); err != nil {
		return err
	}
	landmarks, err := s.readNativeTargetFaceResult(ctx, who, p.landmarksPlan, color)
	if err != nil {
		return err
	}
	skin, err := s.readCompletedNativeTargetSkin(ctx, who, p.skinPlan, color)
	if err != nil {
		return err
	}
	person, err := s.readNativeTargetPersonResult(ctx, who, p.personPlan)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(landmarks, p.landmarks) || !reflect.DeepEqual(skin, p.skin) || !reflect.DeepEqual(person, p.person) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) publishNativeTargetDefectParams(ctx context.Context, who auth.Identity, p nativeTargetDefectPreparation, color TransferColorProfile, publisher v2draft.Publisher) (PublishedDefectParams, error) {
	var zero PublishedDefectParams
	if err := s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return zero, err
	}
	if publisher.DB == nil || publisher.Objects == nil {
		return zero, v2input.ErrInvalid
	}
	var inputs, roots []review.ArtifactRef
	if json.Unmarshal([]byte(p.params.inputs), &inputs) != nil || json.Unmarshal([]byte(p.params.roots), &roots) != nil {
		return zero, v2input.ErrInvalid
	}
	a := p.landmarksPlan.detector.anchor
	document := targetDefectParamsDocument(a.entry.Namespace)
	for _, input := range inputs {
		if input.ID == document {
			return zero, v2input.ErrConflict
		}
	}
	ref, err := publisher.PublishDefectProposalParams(ctx, who, a.request.TenantID, document, a.entry.Binding, []byte(p.landmarks.detector.completed.sourceBody), []byte(p.params.params), inputs)
	if err != nil {
		return zero, err
	}
	if ref.ArtifactID == uuid.Nil || ref.SHA256 != p.params.digest {
		return zero, v2input.ErrConflict
	}
	for _, input := range inputs {
		if input.ID == ref.ArtifactID {
			return zero, v2input.ErrConflict
		}
	}
	if err = s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return zero, err
	}
	return PublishedDefectParams{Ref: ref, Inputs: inputs, ManualRoots: roots}, ctx.Err()
}

func targetDefectParamsDocument(namespace uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte("semantic.defect.params:reference"))
}
