package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualReferencePerson struct {
	source nativeInputManualSourceContinuation
	person nativeInputManualPersonProtectionProof
	policy qualificationPolicySnapshot
}

// Selects server policy and original person producer independently of defects.
// Actual required-person pixel coverage is verified by the existing reader.
func (s SQLTransferSourceReader) prepareNativeInputManualReferencePerson(ctx context.Context, who auth.Identity, request Request, source sourcePreparationProof, registered registeredNativeInputManualInitialProtection, color TransferColorProfile, report v2input.Ref) (nativeInputManualReferencePerson, error) {
	var zero nativeInputManualReferencePerson
	continuation, err := composeNativeInputManualSourceContinuation(ctx, who, request, source, registered, color)
	if err != nil {
		return zero, err
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	retained := registered.published.preparation.plan.prepared.stage.qualification.qualification.policy
	if !reflect.DeepEqual(policy, retained) {
		return zero, v2input.ErrConflict
	}
	selected, err := selectNativeQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	_, _, person, err := s.verifyNativeInputManualPersonProtectionCoverage(ctx, who, request, registered, color, report, selected["mask.person"])
	if err != nil {
		return zero, err
	}
	current, err := s.readRegisteredNativeInputManualInitialProtection(ctx, who, registered.published, color)
	if err != nil {
		return zero, err
	}
	if !sameRegisteredNativeInputManualProtection(ctx, current, registered) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, color, false, source); err != nil {
		return zero, err
	}
	after, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(after, policy) {
		return zero, v2input.ErrConflict
	}
	return nativeInputManualReferencePerson{source: continuation, person: person, policy: policy}, nil
}

// Reference person coverage + original new-native source/protection in one TX.
// Transfer model, targets and full graph admission remain independent.
func (s SQLTransferSourceReader) lockNativeInputManualReferencePerson(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeInputManualReferencePerson, color TransferColorProfile, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !sameRegisteredNativeInputManualProtection(ctx, proof.person.registered, proof.source.protection) || proof.person.root.ArtifactID == uuid.Nil || proof.person.pixels.ArtifactID == uuid.Nil || proof.person.count < 0 {
		return v2input.ErrInvalid
	}
	if !reflect.DeepEqual(proof.policy, proof.source.protection.published.preparation.plan.prepared.stage.qualification.qualification.policy) {
		return v2input.ErrConflict
	}
	if !validQualificationPolicySnapshot(s, who, request.TenantID, proof.policy) {
		return v2input.ErrInvalid
	}
	selected, err := selectNativeQualificationPolicies(proof.policy, []string{"mask.person"})
	if err != nil {
		return err
	}
	expected, err := canonical.Marshal(selected["mask.person"])
	if err != nil {
		return err
	}
	if string(expected) != proof.person.expected {
		return v2input.ErrConflict
	}
	if err := s.lockNativeInputManualSourceContinuation(ctx, tx, who, request, proof.source, color, completed); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, proof.policy.body).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return s.lockNativeInputManualPersonEvidence(ctx, tx, who, request, proof.person)
}
