package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// SQL-only composition; caller rolls back every error. Params registration and
// protected-source authority must still be composed before graph activation.
func (s SQLTransferSourceReader) lockNativeInputManualStageInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, dependencies nativeInputManualDependencies, qualified nativeInputManualStageQualification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	q := qualified.qualification
	d := dependencies.producers.detector
	tenant := d.request.TenantID
	if qualified.dependencySeal == "" || qualified.dependencySeal != dependencies.seal || q.landmarksSeal != dependencies.producers.landmarks.seal || q.skinSeal != dependencies.producers.skin.seal || !validQualificationPolicySnapshot(s, who, tenant, q.policy) || !reflect.DeepEqual(q.policy, d.policy) {
		return v2input.ErrConflict
	}
	selected, err := selectQualificationPolicies(q.policy, []string{"defects.propose"})
	if err != nil {
		return err
	}
	expected := selected["defects.propose"]
	if expected.Backend != "cpu" {
		return v2qualification.ErrUnqualified
	}
	if err := validateTransferMeasuredCosts(q.costs, q.report.metrics); err != nil {
		return err
	}
	if _, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, q.report, expected); err != nil {
		return err
	}
	if err := s.lockNativeInputManualDependenciesInTransaction(ctx, tx, who, dependencies); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", tenant, s.ProjectID, who.UserID, who.SessionID, q.policy.body).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return s.lockDefectQualificationPreparation(ctx, tx, who, tenant, q.report, expected)
}
