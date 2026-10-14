package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type nativeDefectStageQualification struct {
	policy                  qualificationPolicySnapshot
	report                  defectQualificationPreparationProof
	costs                   v2input.SemanticQualifiedCosts
	landmarksSeal, skinSeal string
}

// Selects current CPU defects qualification independently of FAN/BiSeNet.
// Imported or caller-selected expected policies cannot replace server policy.
func (s SQLTransferSourceReader) prepareNativeDefectStageQualification(ctx context.Context, who auth.Identity, prepared nativeDefectFacePreparation, color TransferColorProfile, report v2input.Ref, costs v2input.SemanticQualifiedCosts) (nativeDefectStageQualification, error) {
	var zero nativeDefectStageQualification
	if err := verifyNativeFacePlanSeal(ctx, prepared.landmarksPlan); err != nil {
		return zero, err
	}
	if err := verifyNativeSkinPlanSeal(ctx, prepared.skinPlan); err != nil {
		return zero, err
	}
	if err := verifyNativeDefectPreparedParams(ctx, prepared); err != nil {
		return zero, err
	}
	if prepared.landmarksPlan.detector.seal != prepared.skinPlan.parser.detector.seal {
		return zero, v2input.ErrConflict
	}
	tenant := prepared.landmarksPlan.shell.Tenant
	policy, err := s.readQualificationPolicySnapshot(ctx, who, tenant)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(policy, prepared.landmarksPlan.detector.policy) {
		return zero, v2input.ErrConflict
	}
	selected, err := selectQualificationPolicies(policy, []string{"defects.propose"})
	if err != nil {
		return zero, err
	}
	expected := selected["defects.propose"]
	if expected.Backend != "cpu" {
		return zero, v2qualification.ErrUnqualified
	}
	qualified, err := s.readDefectExecutionQualification(ctx, who, tenant, report, expected, costs)
	if err != nil {
		return zero, err
	}
	landmarks, err := s.readCompletedNativeLandmarks(ctx, who, prepared.landmarksPlan, color)
	if err != nil {
		return zero, err
	}
	skin, err := s.readCompletedNativeSkin(ctx, who, prepared.skinPlan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(landmarks, prepared.landmarks) || !reflect.DeepEqual(skin, prepared.skin) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, tenant, qualified, expected); err != nil {
		return zero, err
	}
	if err := s.recheckNativePlanPolicy(ctx, who, tenant, policy); err != nil {
		return zero, err
	}
	return nativeDefectStageQualification{policy: policy, report: qualified, costs: costs, landmarksSeal: prepared.landmarksPlan.seal, skinSeal: prepared.skinPlan.seal}, nil
}
