package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type nativeInputManualStageQualification struct {
	qualification  nativeDefectStageQualification
	dependencySeal string
}

// Selects current server-owned CPU qualification for the exact original manual
// dependency packet. Does not convert it into a bare-face or imported proof.
func (s SQLTransferSourceReader) prepareNativeInputManualStageQualification(ctx context.Context, who auth.Identity, dependencies nativeInputManualDependencies, color TransferColorProfile, report v2input.Ref, costs v2input.SemanticQualifiedCosts) (nativeInputManualStageQualification, error) {
	return s.prepareNativeInputManualQualification(ctx, who, dependencies, report, costs, func() error {
		current, err := s.readNativeInputSavedManualInputs(ctx, who, dependencies.producers, color)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, dependencies.selection) {
			return v2input.ErrConflict
		}
		return ctx.Err()
	})
}

// Internal common CPU policy/report checks. Its callers own the distinct saved
// or initial accepted-Params recheck; no external callback grants authority.
func (s SQLTransferSourceReader) prepareNativeInputManualQualification(ctx context.Context, who auth.Identity, dependencies nativeInputManualDependencies, report v2input.Ref, costs v2input.SemanticQualifiedCosts, recheck func() error) (nativeInputManualStageQualification, error) {
	var zero nativeInputManualStageQualification
	if err := verifyNativeInputManualProducerBindings(ctx, who, dependencies.producers, dependencies.selection); err != nil {
		return zero, err
	}
	digest, err := s.nativeInputManualDependencyDigest(ctx, dependencies)
	if err != nil {
		return zero, err
	}
	if dependencies.seal == "" || digest != dependencies.seal {
		return zero, v2input.ErrConflict
	}
	if err := s.verifyNativeInputManualCompletedPackets(ctx, dependencies); err != nil {
		return zero, err
	}
	d := dependencies.producers.detector
	policy, err := s.readQualificationPolicySnapshot(ctx, who, d.request.TenantID)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(policy, d.policy) {
		return zero, v2input.ErrConflict
	}
	selected, err := selectNativeQualificationPolicies(policy, []string{"defects.propose"})
	if err != nil {
		return zero, err
	}
	expected := selected["defects.propose"]
	if expected.Backend != "cpu" {
		return zero, v2qualification.ErrUnqualified
	}
	qualified, err := s.readDefectExecutionQualification(ctx, who, d.request.TenantID, report, expected, costs)
	if err != nil {
		return zero, err
	}
	if recheck == nil {
		return zero, v2input.ErrInvalid
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, d.request.TenantID, qualified, expected); err != nil {
		return zero, err
	}
	if err := s.recheckNativePlanPolicy(ctx, who, d.request.TenantID, policy); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeInputManualStageQualification{qualification: nativeDefectStageQualification{policy: policy, report: qualified, costs: costs, landmarksSeal: dependencies.producers.landmarks.seal, skinSeal: dependencies.producers.skin.seal}, dependencySeal: dependencies.seal}, nil
}
