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

type nativeInputReferencePerson struct {
	source         nativeInputSourceContinuation
	person         nativeInputPersonProtectionProof
	policy         qualificationPolicySnapshot
	originalPinned bool
}

// Selects server policy and original person producer independently of defects.
// Actual required-person pixel coverage is verified by the existing reader.
func (s SQLTransferSourceReader) prepareNativeInputReferencePerson(ctx context.Context, who auth.Identity, request Request, source sourcePreparationProof, registered registeredNativeInputProtection, color TransferColorProfile, report v2input.Ref) (nativeInputReferencePerson, error) {
	return s.prepareNativeInputReferencePersonMode(ctx, who, request, source, registered, color, report, false)
}

func (s SQLTransferSourceReader) prepareNativeInputReferencePersonMode(ctx context.Context, who auth.Identity, request Request, source sourcePreparationProof, registered registeredNativeInputProtection, color TransferColorProfile, report v2input.Ref, originalPinned bool) (nativeInputReferencePerson, error) {
	var zero nativeInputReferencePerson
	continuation, err := composeNativeInputSourceContinuation(ctx, who, request, source, registered, color)
	if err != nil {
		return zero, err
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	retained := registered.published.preparation.plan.prepared.qualification.policy
	if !reflect.DeepEqual(policy, retained) {
		return zero, v2input.ErrConflict
	}
	selected, err := selectQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	_, _, person, err := s.verifyNativeInputPersonProtectionCoverageMode(ctx, who, request, registered, color, report, selected["mask.person"], originalPinned)
	if err != nil {
		return zero, err
	}
	current, err := s.readRegisteredNativeInputProtection(ctx, who, registered.published, color)
	if err != nil {
		return zero, err
	}
	if !sameRegisteredNativeInputProtection(ctx, current, registered) {
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
	return nativeInputReferencePerson{source: continuation, person: person, policy: policy, originalPinned: originalPinned}, nil
}

// Reference person coverage + original new-native source/protection in one TX.
// Transfer model, targets and full graph admission remain independent.
func (s SQLTransferSourceReader) lockNativeInputReferencePerson(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeInputReferencePerson, color TransferColorProfile, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if proof.originalPinned || tx == nil || !sameRegisteredNativeInputProtection(ctx, proof.person.registered, proof.source.protection) || proof.person.root.ArtifactID == uuid.Nil || proof.person.pixels.ArtifactID == uuid.Nil || proof.person.count < 0 {
		return v2input.ErrInvalid
	}
	if !reflect.DeepEqual(proof.policy, proof.source.protection.published.preparation.plan.prepared.qualification.policy) {
		return v2input.ErrConflict
	}
	if !validQualificationPolicySnapshot(s, who, request.TenantID, proof.policy) {
		return v2input.ErrInvalid
	}
	selected, err := selectQualificationPolicies(proof.policy, []string{"mask.person"})
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
	if err := s.lockNativeInputSourceContinuation(ctx, tx, who, request, proof.source, color, completed); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, proof.policy.body).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return s.lockNativeInputPersonEvidence(ctx, tx, who, request, proof.person)
}
