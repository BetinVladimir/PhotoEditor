package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Backend-only reference evidence. Complete transfer target/source/policy and
// graph admission remain caller obligations; this is not a queued ticket.
type nativeReferenceProtectionProof struct {
	selection nativeSelectionSeparationProof
	person    nativePersonProtectionProof
	policy    qualificationPolicySnapshot
	policySHA string
}

func (s SQLTransferSourceReader) prepareNativeReferenceProtection(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, root, personReport v2input.Ref, expected v2qualification.Expected) (nativeReferenceProtectionProof, error) {
	expected = ownQualificationExpected(expected)
	var zero nativeReferenceProtectionProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	selectedPolicies, err := selectQualificationPolicies(policy, []string{"defects.propose", "face.detect", "face.landmarks", "face.parse", "mask.person"})
	if err != nil || !sameQualificationPolicyExpected(selectedPolicies["mask.person"], expected) {
		return zero, v2qualification.ErrUnqualified
	}
	policySHA, err := canonical.Hash(json.RawMessage(policy.body))
	if err != nil {
		return zero, err
	}
	_, selection, err := s.verifyNativeSelectionSeparation(ctx, who, request, key, intent, snapshot, task, root)
	if err != nil {
		return zero, err
	}
	_, _, person, err := s.verifyNativePersonProtectionCoverage(ctx, who, request, key, intent, snapshot, task, root, personReport, expected)
	if err != nil {
		return zero, err
	}
	if selection.registered != person.registered {
		return zero, v2input.ErrConflict
	}
	if err := s.nativePolicyReady(ctx, who, request, selection.registered.parent, policySHA); err != nil {
		return zero, err
	}
	currentPolicy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	if currentPolicy != policy {
		return zero, v2input.ErrConflict
	}
	// Later person object IO must not invalidate earlier selection pins.
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	currentRoot, err := catalog.Lookup(ctx, who, request.TenantID, v2draft.LayerRef{Ref: selection.root, Contract: "MaskRef"})
	if err != nil {
		return zero, err
	}
	currentPixels, err := catalog.Lookup(ctx, who, request.TenantID, v2draft.LayerRef{Ref: selection.pixels, Contract: "MaskPixels", ParentRef: selection.root})
	if err != nil {
		return zero, err
	}
	if currentRoot != selection.rootObject || currentPixels != selection.pixelsObject {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeReferenceProtectionProof{selection: selection, person: person, policy: policy, policySHA: policySHA}, nil
}

// Both gates execute in the same caller-owned scheduler TX, with no object IO.
// Any error requires rollback, including when the first component succeeded.
func (s SQLTransferSourceReader) lockNativeReferenceProtection(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeReferenceProtectionProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || proof.selection.count <= 0 || proof.selection.registered != proof.person.registered {
		return v2input.ErrInvalid
	}
	if !validQualificationPolicySnapshot(s, who, request.TenantID, proof.policy) {
		return v2input.ErrInvalid
	}
	if err := matchNativeReferencePolicy(proof); err != nil {
		return err
	}
	if err := s.lockNativeSelectionSeparation(ctx, tx, who, request, proof.selection); err != nil {
		return err
	}
	if err := s.lockNativePersonProtection(ctx, tx, who, request.TenantID, request.Intent.Reference, proof.person); err != nil {
		return err
	}
	var ready bool
	parent := proof.selection.registered.parent
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_native_policy($1,$2,$3,$4,$5,$6::jsonb,$7,$8::jsonb)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, parent.result.task, parent.anchor, proof.policySHA, proof.policy.body).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) nativePolicyReady(ctx context.Context, who auth.Identity, request Request, parent nativeDefectParentProof, sha string) error {
	return tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var ready bool
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_defect_native_policy_ready($1,$2,$3,$4,$5,$6::jsonb,$7)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, parent.result.task, parent.anchor, sha).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
		return nil
	})
}
