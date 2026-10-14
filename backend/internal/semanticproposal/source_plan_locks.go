package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Locks selected original source versions. Caller composes complete inventory,
// authorization and admission, repeats after inventory locks, rolls back errors.
func LockPreparedSourcesInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, plan WorkflowPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.TenantID == uuid.Nil || request.RunID == uuid.Nil || plan.project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tx == nil || plan.tenant != request.TenantID || plan.actor != who.UserID || who.UserID != request.ActorID || plan.run != request.RunID {
		return v2input.ErrInvalid
	}
	if err := validateNativeTransferPlanScope(who, request, plan); err != nil {
		return err
	}
	if plan.nativeReferenceRequired != (plan.nativeReference != nil) {
		return v2input.ErrInvalid
	}
	if (plan.sourcesRequired || plan.nativeReferenceRequired) && len(plan.sources) == 0 {
		return v2input.ErrInvalid
	}
	if len(plan.sources) == 0 {
		return nil
	} // internal scaffold only
	if len(plan.inventory.Targets) < 1 || len(plan.inventory.Targets) > 5000 || len(plan.sources) != 2*len(plan.inventory.Targets) {
		return v2input.ErrInvalid
	}
	owned := append([]sourcePreparationProof(nil), plan.sources...)
	selected := make([]sourcePreparationProof, 0, len(owned))
	seen := map[v2input.SessionVersionBinding]sourcePreparationProof{}
	for i, proof := range owned {
		binding := request.Intent.Reference
		if i%2 == 1 {
			binding = plan.inventory.Targets[i/2]
		}
		if proof.tenant != request.TenantID || proof.project != plan.project || proof.actor != who.UserID || proof.session != who.SessionID || proof.bucket == "" || proof.binding != binding {
			return v2input.ErrInvalid
		}
		if plan.nativeReferenceRequired && i%2 == 0 && proof != plan.nativeReference.source {
			return v2input.ErrConflict
		}
		if _, err := domain.ParseDigest(proof.iccSHA); err != nil {
			return v2input.ErrInvalid
		}
		if _, err := marshalSourceEvidencePins(proof); err != nil {
			return err
		}
		if previous, ok := seen[binding]; ok {
			if previous != proof {
				return v2input.ErrConflict
			}
			continue
		}
		seen[binding] = proof
		selected = append(selected, proof)
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].binding.Source.ArtifactID != selected[j].binding.Source.ArtifactID {
			return selected[i].binding.Source.ArtifactID.String() < selected[j].binding.Source.ArtifactID.String()
		}
		return selected[i].binding.AssetVersionID.String() < selected[j].binding.AssetVersionID.String()
	})
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", request.TenantID.String()); err != nil {
		return err
	}
	for _, proof := range selected {
		reader := SQLTransferSourceReader{ProjectID: plan.project, Bucket: proof.bucket}
		if err := reader.lockSourcePreparationProof(ctx, tx, who, request.TenantID, proof.binding, proof); err != nil {
			return err
		}
	}
	if plan.nativeReferenceRequired {
		native := plan.nativeReference
		reader := SQLTransferSourceReader{ProjectID: plan.project, Bucket: native.source.bucket}
		if err := reader.lockNativeSourceContinuation(ctx, tx, who, request, *native, TransferColorProfile{sha: native.source.iccSHA}); err != nil {
			return err
		}
	}
	return ctx.Err()
}
