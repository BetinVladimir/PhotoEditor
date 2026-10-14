package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Prepares the exact original person mask as permanent defect protection.
// The original authority is retained with Params, not replaced by the projected
// protected declaration. Activation still requires independent SQL admission.
func (s SQLTransferSourceReader) prepareNativeDefectPersonFaceParams(ctx context.Context, who auth.Identity, landmarks nativeFaceDraftPlan, skin nativeSkinDraftPlan, color TransferColorProfile, diameter [2]float64, person nativePersonInputAuthority) (nativeDefectFacePreparation, error) {
	var zero nativeDefectFacePreparation
	if err := verifyNativeFacePlanSeal(ctx, landmarks); err != nil {
		return zero, err
	}
	if err := verifyNativeSkinPlanSeal(ctx, skin); err != nil {
		return zero, err
	}
	if landmarks.detector.seal != skin.parser.detector.seal {
		return zero, v2input.ErrConflict
	}
	request := landmarks.detector.request
	image, err := s.Read(ctx, who, request.TenantID, request.Intent.Reference)
	if err != nil {
		return zero, err
	}
	mask, err := s.prepareNativePersonProtectionInput(ctx, who, request, person, image)
	if err != nil {
		return zero, err
	}
	prepared, err := s.prepareNativeDefectFaceParams(ctx, who, landmarks, skin, color, diameter, &mask, nil, nil)
	if err != nil {
		return zero, err
	}
	prepared.person = &person
	if err := verifyNativeDefectPreparedParams(ctx, prepared); err != nil {
		return zero, err
	}
	if err := s.recheckNativeDefectPersonInput(ctx, who, prepared); err != nil {
		return zero, err
	}
	return prepared, nil
}

// Pure comparison of exact Params and retained authority. Nil preserves the
// existing face-only and separately authorized manual compilation contracts.
func verifyNativeDefectPersonInput(ctx context.Context, prepared nativeDefectFacePreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if prepared.person == nil {
		return nil
	}
	p := *prepared.person
	parent := prepared.landmarksPlan.detector
	scope := SQLTransferSourceReader{ProjectID: parent.policy.project, Bucket: parent.prepared.source.bucket}
	who := auth.Identity{UserID: parent.policy.actor, SessionID: parent.policy.session}
	if err := scope.validateNativePersonInputAuthority(ctx, who, parent.request, p); err != nil {
		return err
	}
	if !reflect.DeepEqual(p.policy, parent.policy) {
		return v2input.ErrConflict
	}
	mask, err := projectNativePersonProtectionInput(ctx, p, json.RawMessage(prepared.landmarks.detector.sourceBody))
	if err != nil {
		return err
	}
	selected, err := selectQualificationPolicies(p.policy, []string{"mask.person"})
	if err != nil {
		return err
	}
	if mask.ModelLockID == nil || *mask.ModelLockID != selected["mask.person"].ModelLockSHA {
		return v2input.ErrConflict
	}
	expected, err := canonical.Marshal(selected["mask.person"])
	if err != nil {
		return err
	}
	if p.expected != string(expected) {
		return v2input.ErrConflict
	}
	var declared struct {
		Permanent   *review.ProtectionMask  `json:"permanent"`
		Protections []review.ProtectionMask `json:"protections"`
	}
	var roots []review.ArtifactRef
	if json.Unmarshal([]byte(prepared.params.params), &declared) != nil || json.Unmarshal([]byte(prepared.params.roots), &roots) != nil {
		return v2input.ErrInvalid
	}
	if !reflect.DeepEqual(declared.Permanent, &mask) || len(declared.Protections) != 0 || len(roots) != 0 {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Performs physical reads outside SQL transactions; replay must retain the
// original root, report and immutable object versions, never a new candidate.
func (s SQLTransferSourceReader) recheckNativeDefectPersonInput(ctx context.Context, who auth.Identity, prepared nativeDefectFacePreparation) error {
	if err := verifyNativeDefectPersonInput(ctx, prepared); err != nil {
		return err
	}
	if prepared.person == nil {
		return nil
	}
	_, err := s.prepareNativePersonProtectionInput(ctx, who, prepared.landmarksPlan.detector.request, *prepared.person, json.RawMessage(prepared.landmarks.detector.sourceBody))
	return err
}

// Exact pixel inventory for this independently proven input; original root and
// report stay in the authority, while worker Params consume only original NPY.
func nativeDefectPersonInputPins(ctx context.Context, prepared nativeDefectFacePreparation) ([]nativeDefectInputPin, error) {
	if err := verifyNativeDefectPersonInput(ctx, prepared); err != nil {
		return nil, err
	}
	if prepared.person == nil {
		return nil, nil
	}
	p := prepared.person
	if p.pixels.ArtifactID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	return []nativeDefectInputPin{{contract: "NpyArray", pin: defectEvidenceObject{ref: p.pixels, object: p.objects[1]}}}, nil
}
