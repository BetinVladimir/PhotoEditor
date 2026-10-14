package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Composes current CPU qualification, policy and exact native dependencies in
// one transaction. No object IO/commit or activation; rollback every error.
func (s SQLTransferSourceReader) lockNativeDefectStageInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, dependencies nativeDefectDependencies, qualification nativeDefectStageQualification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	prepared := dependencies.preparation
	tenant := prepared.landmarksPlan.shell.Tenant
	if qualification.landmarksSeal == "" || qualification.landmarksSeal != prepared.landmarksPlan.seal || qualification.skinSeal != prepared.skinPlan.seal || !validQualificationPolicySnapshot(s, who, tenant, qualification.policy) || !reflect.DeepEqual(qualification.policy, prepared.landmarksPlan.detector.policy) {
		return v2input.ErrConflict
	}
	selected, err := selectQualificationPolicies(qualification.policy, []string{"defects.propose"})
	if err != nil {
		return err
	}
	expected := selected["defects.propose"]
	if expected.Backend != "cpu" {
		return v2qualification.ErrUnqualified
	}
	if err := validateTransferMeasuredCosts(qualification.costs, qualification.report.metrics); err != nil {
		return err
	}
	// Validate the complete report before any SQL; its live pin is locked below.
	if _, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, qualification.report, expected); err != nil {
		return err
	}
	if err := s.lockNativeDefectDependenciesInTransaction(ctx, tx, who, dependencies); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", tenant, s.ProjectID, who.UserID, who.SessionID, qualification.policy.body).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return s.lockDefectQualificationPreparation(ctx, tx, who, tenant, qualification.report, expected)
}
