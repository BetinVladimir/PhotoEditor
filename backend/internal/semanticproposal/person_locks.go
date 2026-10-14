package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// LockPersonProducerInTransaction retains producer/profile and exact validated
// Params/PersonReport object pins.
// It also holds qualification policy/report and the tenant inventory barrier
// while repeating candidate discovery. Any error requires transaction rollback.
func LockPersonProducerInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, plan WorkflowPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || who.UserID != request.ActorID || plan.tenant != request.TenantID || plan.actor != request.ActorID || plan.RunID() != request.RunID || len(plan.tasks) == 0 {
		return v2input.ErrInvalid
	}
	if err := validateNativeTransferPlanScope(who, request, plan); err != nil {
		return err
	}
	if plan.nativeReferenceRequired {
		native := plan.nativeReference
		reader := SQLTransferSourceReader{ProjectID: plan.project, Bucket: native.source.bucket}
		if err := reader.lockNativeSourceContinuation(ctx, tx, who, request, *native, TransferColorProfile{sha: native.source.iccSHA}); err != nil {
			return err
		}
		return lockPersonProtectionProofsInTransaction(ctx, tx, who, request.TenantID, plan.project, plan.person)
	}
	if len(plan.person) == 0 {
		return nil
	}
	if len(plan.person) != 2*len(plan.tasks) {
		return v2input.ErrInvalid
	}
	return lockPersonProtectionProofsInTransaction(ctx, tx, who, request.TenantID, plan.ProjectID(), plan.person)
}

// Shared locking for real independently prepared coverage proofs. The caller
// retains operation-specific cardinality, source/task scope and content checks.
func lockPersonProtectionProofsInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID, projectID uuid.UUID, proofs []personProtectionProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenantID == uuid.Nil || projectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || len(proofs) < 1 || len(proofs) > 10000 {
		return v2input.ErrInvalid
	}
	owned := append([]personProtectionProof(nil), proofs...)
	for _, proof := range owned {
		reader := SQLTransferSourceReader{ProjectID: projectID, Bucket: proof.protectionEvidence.bucket}
		if !validPersonPreparationScope(reader, who, tenantID, proof.Binding, proof) {
			return v2input.ErrInvalid
		}
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	seen := map[personProtectionProof]bool{}
	entries := make([]personProtectionProof, 0, len(owned))
	for _, proof := range owned {
		if !seen[proof] {
			seen[proof] = true
			entries = append(entries, proof)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Binding.AssetVersionID != b.Binding.AssetVersionID {
			return a.Binding.AssetVersionID.String() < b.Binding.AssetVersionID.String()
		}
		if a.Root != b.Root {
			return a.Root.ArtifactID.String()+a.Root.SHA256 < b.Root.ArtifactID.String()+b.Root.SHA256
		}
		if a.Protection != b.Protection {
			return a.Protection.ArtifactID.String()+a.Protection.SHA256 < b.Protection.ArtifactID.String()+b.Protection.SHA256
		}
		return a.Expected+a.Evidence+a.Qualification < b.Expected+b.Evidence+b.Qualification
	})
	lockAll := func() error {
		for _, proof := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			reader := SQLTransferSourceReader{ProjectID: projectID, Bucket: proof.protectionEvidence.bucket}
			if err := reader.lockProtectionPreparationProof(ctx, tx, who, tenantID, proof.Binding, proof.protectionEvidence); err != nil {
				return err
			}
			var ready bool
			err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", tenantID, projectID, who.UserID, who.SessionID, proof.Binding.AssetVersionID, proof.Binding.Source.ArtifactID, proof.Binding.Source.SHA256, proof.Root.ArtifactID, proof.Root.SHA256, proof.Child.ArtifactID, proof.Child.SHA256, proof.Expected, proof.Evidence).Scan(&ready)
			if err != nil {
				return err
			}
			if !ready {
				return v2input.ErrConflict
			}
			err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_qualification($1,$2,$3,$4,$5,$6)", tenantID, projectID, who.UserID, who.SessionID, proof.Expected, proof.Qualification).Scan(&ready)
			if err != nil {
				return err
			}
			if !ready {
				return v2input.ErrConflict
			}
		}
		return nil
	}
	if err := lockAll(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_inventory($1,$2,$3,$4)", tenantID, projectID, who.UserID, who.SessionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	if err := lockAll(); err != nil {
		return err
	}
	for _, proof := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_person_candidate_matches($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", tenantID, projectID, who.UserID, who.SessionID, proof.Binding.AssetVersionID, proof.Binding.Source.ArtifactID, proof.Binding.Source.SHA256, proof.Root.ArtifactID, proof.Root.SHA256, proof.Child.ArtifactID, proof.Child.SHA256, proof.Expected).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
	}
	return nil
}
