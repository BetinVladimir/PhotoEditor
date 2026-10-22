package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
)

// Dedicated target authority; shared report proof defines qualification only.
// This does not authorize a task, reserve quota, or lock dependency producers.
type nativeTargetDefectQualification struct {
	preparationSeal string
	policy          qualificationPolicySnapshot
	report          defectQualificationPreparationProof
	costs           v2input.SemanticQualifiedCosts
	seal            string
}

func (s SQLTransferSourceReader) targetDefectQualificationDigest(ctx context.Context, who auth.Identity, p nativeTargetDefectPreparation, q nativeTargetDefectQualification) (string, error) {
	if err := verifyNativeTargetDefectPreparation(ctx, p); err != nil {
		return "", err
	}
	tenant := p.landmarksPlan.shell.Tenant
	if q.preparationSeal != p.seal || !validQualificationPolicySnapshot(s, who, tenant, q.policy) || !reflect.DeepEqual(q.policy, p.landmarksPlan.detector.detector.policy) || q.costs.VRAMBytes != 0 {
		return "", v2input.ErrConflict
	}
	selected, err := selectNativeQualificationPolicies(q.policy, []string{"defects.propose"})
	if err != nil {
		return "", err
	}
	expected := selected["defects.propose"]
	if expected.Backend != "cpu" {
		return "", v2qualification.ErrUnqualified
	}
	report, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, q.report, expected)
	if err != nil {
		return "", err
	}
	if err := validateTransferMeasuredCosts(q.costs, q.report.metrics); err != nil {
		return "", err
	}
	return canonical.Hash(map[string]any{"preparation": q.preparationSeal, "policy": q.policy.body, "report": string(report), "expected": q.report.expected, "metrics": q.report.metrics, "object": q.report.report.object, "costs": q.costs})
}

func (s SQLTransferSourceReader) verifyNativeTargetDefectQualification(ctx context.Context, who auth.Identity, p nativeTargetDefectPreparation, q nativeTargetDefectQualification) error {
	digest, err := s.targetDefectQualificationDigest(ctx, who, p, q)
	if err != nil {
		return err
	}
	if q.seal == "" || digest != q.seal {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// An explicit server-selected report is verified against the current policy.
// Object reads and report/producers/policy rechecks precede any transaction.
func (s SQLTransferSourceReader) prepareNativeTargetDefectQualification(ctx context.Context, who auth.Identity, p nativeTargetDefectPreparation, color TransferColorProfile, report v2input.Ref, costs v2input.SemanticQualifiedCosts) (nativeTargetDefectQualification, error) {
	var zero nativeTargetDefectQualification
	if err := s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return zero, err
	}
	tenant := p.landmarksPlan.shell.Tenant
	policy, err := s.readQualificationPolicySnapshot(ctx, who, tenant)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(policy, p.landmarksPlan.detector.detector.policy) || costs.VRAMBytes != 0 {
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
	proof, err := s.readDefectExecutionQualification(ctx, who, tenant, report, expected, costs)
	if err != nil {
		return zero, err
	}
	q := nativeTargetDefectQualification{preparationSeal: p.seal, policy: policy, report: proof, costs: costs}
	q.seal, err = s.targetDefectQualificationDigest(ctx, who, p, q)
	if err != nil {
		return zero, err
	}
	if err := s.recheckNativeTargetDefectQualification(ctx, who, p, q, color); err != nil {
		return zero, err
	}
	return q, ctx.Err()
}

func (s SQLTransferSourceReader) recheckNativeTargetDefectQualification(ctx context.Context, who auth.Identity, p nativeTargetDefectPreparation, q nativeTargetDefectQualification, color TransferColorProfile) error {
	if err := s.verifyNativeTargetDefectQualification(ctx, who, p, q); err != nil {
		return err
	}
	if err := s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return err
	}
	selected, err := selectNativeQualificationPolicies(q.policy, []string{"defects.propose"})
	if err != nil {
		return err
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, p.landmarksPlan.shell.Tenant, q.report, selected["defects.propose"]); err != nil {
		return err
	}
	return s.recheckNativePlanPolicy(ctx, who, p.landmarksPlan.shell.Tenant, q.policy)
}
