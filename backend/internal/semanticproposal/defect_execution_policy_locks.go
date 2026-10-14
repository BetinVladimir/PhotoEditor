package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Selected expectations must all derive from one original DBA policy snapshot.
func matchDefectExecutionPolicySnapshot(prepared defectMaskExecutionPreparation, policy qualificationPolicySnapshot) error {
	var defect v2qualification.Expected
	var facesAndMasks [4]v2qualification.Expected
	if json.Unmarshal([]byte(prepared.face.source.expected), &defect) != nil || json.Unmarshal([]byte(prepared.expected), &facesAndMasks) != nil {
		return v2input.ErrInvalid
	}
	required := []string{"defects.propose", "face.landmarks", "face.detect", "face.parse"}
	selected, err := selectQualificationPolicies(policy, required)
	if err != nil {
		return err
	}
	expected := append([]v2qualification.Expected{defect}, facesAndMasks[:]...)
	for _, value := range expected {
		chosen, exists := selected[value.Operation]
		if !exists || !sameQualificationPolicyExpected(chosen, value) {
			return v2qualification.ErrUnqualified
		}
	}
	if defect.Operation != "defects.propose" || facesAndMasks[0].Operation != "face.landmarks" || facesAndMasks[1].Operation != "face.detect" || facesAndMasks[2].Operation != "face.parse" || facesAndMasks[3].Operation != "face.detect" {
		return v2qualification.ErrUnqualified
	}
	return nil
}

// Selected evidence and full policy catalog locks; caller must compose final
// authorization/admission. This does not commit or queue tasks.
func (s SQLTransferSourceReader) lockDefectExecutionPolicyInventoryInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectMaskExecutionPreparation, policy qualificationPolicySnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !validQualificationPolicySnapshot(s, who, tenant, policy) {
		return v2input.ErrInvalid
	}
	if err := matchDefectExecutionPolicySnapshot(prepared, policy); err != nil {
		return err
	}
	if err := s.lockDefectExecutionInventoryInTransaction(ctx, tx, who, tenant, binding, prepared); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", tenant, s.ProjectID, who.UserID, who.SessionID, policy.body).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return s.lockDefectExecutionInventoryInTransaction(ctx, tx, who, tenant, binding, prepared)
}
