package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type defectPersonAdmissionPreparation struct {
	plan   defectPreparationDraftPlan
	person personProtectionProof
}

// Checks content and scope only; proof must be independently prepared and
// rechecked before retention, then locked again before any activation.
func (s SQLTransferSourceReader) validateDefectPersonAdmission(ctx context.Context, who auth.Identity, plan defectPreparationDraftPlan, proof personProtectionProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validPersonPreparationScope(s, who, plan.request.TenantID, plan.request.Intent.Reference, proof) {
		return v2input.ErrInvalid
	}
	if !validQualificationPolicySnapshot(s, who, plan.request.TenantID, plan.execution.policy) {
		return v2input.ErrInvalid
	}
	selected, err := selectQualificationPolicies(plan.execution.policy, []string{"mask.person"})
	if err != nil {
		return err
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(proof.Expected), &expected) != nil || !sameQualificationPolicyExpected(expected, selected["mask.person"]) {
		return v2qualification.ErrUnqualified
	}
	if len(plan.protectionSource) < 2 || len(plan.protectionSource) > 1048576 {
		return v2input.ErrInvalid
	}
	var source review.DefectSource
	if json.Unmarshal([]byte(plan.protectionSource), &source) != nil || source.Image.ID != proof.Binding.Source.ArtifactID || source.Image.SHA256 != proof.Binding.Source.SHA256 {
		return v2input.ErrInvalid
	}
	masks, err := review.DefectProposalMaskInputs(review.ArtifactRef{ID: plan.params.params.ArtifactID, SHA256: plan.params.params.SHA256}, []byte(plan.params.preparation.params.params), source)
	if err != nil {
		return err
	}
	pixel := proof.protectionEvidence.pixels.ref
	for _, mask := range masks {
		if mask.Protected && mask.Artifact.ID == pixel.ArtifactID && mask.Artifact.SHA256 == pixel.SHA256 {
			return nil
		}
	}
	return v2input.ErrInvalid
}

func (s SQLTransferSourceReader) prepareDefectPersonAdmission(ctx context.Context, who auth.Identity, plan defectPreparationDraftPlan, proof personProtectionProof) (defectPersonAdmissionPreparation, error) {
	var zero defectPersonAdmissionPreparation
	if err := s.validateDefectPersonAdmission(ctx, who, plan, proof); err != nil {
		return zero, err
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(proof.Expected), &expected) != nil {
		return zero, v2input.ErrInvalid
	}
	current, err := s.selectPersonProtectionProof(ctx, who, plan.request.TenantID, proof.Binding, proof.Protection, proof.Report, expected)
	if err != nil {
		return zero, err
	}
	if current != proof {
		return zero, v2input.ErrConflict
	}
	return defectPersonAdmissionPreparation{plan: plan, person: current}, nil
}

// Composes exact native draft, source/policy/producer/Params and one real person
// coverage proof under the same SQL transaction. It does not activate the task.
func (s SQLTransferSourceReader) lockDefectPersonAdmissionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, prepared defectPersonAdmissionPreparation) error {
	if tx == nil {
		return defectAdmissionStageError(defectStagePersonTransaction, v2input.ErrInvalid)
	}
	if err := s.validateDefectPersonAdmission(ctx, who, prepared.plan, prepared.person); err != nil {
		return defectAdmissionStageError(defectStagePersonValidation, err)
	}
	if _, err := s.beginDefectPreparationDraftInTransaction(ctx, tx, who, prepared.plan); err != nil {
		return defectAdmissionStageError(defectStageBeginDraft, err)
	}
	if err := lockPersonProtectionProofsInTransaction(ctx, tx, who, prepared.plan.request.TenantID, s.ProjectID, []personProtectionProof{prepared.person}); err != nil {
		return defectAdmissionStageError(defectStagePersonProofs, err)
	}
	person, err := s.marshalDefectPersonAuthority(ctx, who, prepared)
	if err != nil {
		return defectAdmissionStageError(defectStagePersonPacket, err)
	}
	content, err := marshalDefectActivationContent(ctx, prepared.plan)
	if err != nil {
		return defectAdmissionStageError(defectStageActivationContent, err)
	}
	binding := prepared.plan.request.Intent.Reference
	var matches bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_person_authority($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb)", prepared.plan.request.TenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, string(person), string(content)).Scan(&matches); err != nil {
		return defectAdmissionStageError(defectStagePersonQuery, err)
	}
	if !matches {
		return defectAdmissionStageError(defectStagePersonState, v2input.ErrConflict)
	}
	return defectAdmissionStageError(defectStagePersonPolicy, s.lockDefectPolicyExecutionInTransaction(ctx, tx, who, prepared.plan.request.TenantID, prepared.plan.request.Intent.Reference, prepared.plan.execution))
}
