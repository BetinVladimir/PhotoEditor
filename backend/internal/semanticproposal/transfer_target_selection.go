package semanticproposal

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Only an empty authorized candidate set is eligible for future producer
// orchestration. Corrupt, ambiguous or unqualified existing evidence must not
// be replaced by a newly generated producer to hide the original failure.
var errTransferProtectionUnavailable = fmt.Errorf("transfer protection producer unavailable: %w", v2qualification.ErrUnqualified)

// Unique exact-source origin is a locator only. Physical mask validation and
// independent person coverage follow; ambiguity never selects newest/first.
func (s SQLTransferSourceReader) transferTargetProtectionCandidate(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding) (personCandidate, error) {
	var zero personCandidate
	var values []personCandidate
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{binding.Source.SHA256, binding.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT root.id,root.sha256
FROM photo.artifacts root JOIN photo.artifacts child ON child.tenant_id=root.tenant_id AND child.created_by_task_id=root.created_by_task_id
WHERE root.tenant_id=$1 AND root.output_role IN('mask_protected_ref','defect_protection_ref','transformed_mask_ref','mask')
AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
AND photo.semantic_proposal_protection_origin($1,$2,$3,$4,$5,$6,$7,root.id,root.sha256,child.id,child.sha256)
ORDER BY root.id LIMIT 2`, tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			if count >= 2 {
				return v2input.ErrConflict
			}
			var out personCandidate
			if err := rows.Scan(&out.root.ArtifactID, &out.root.SHA256); err != nil {
				return err
			}
			values = append(values, out)
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	if len(values) == 0 {
		return zero, errTransferProtectionUnavailable
	}
	if len(values) != 1 {
		return zero, v2input.ErrConflict
	}
	// A canonical producer may own several mask arrays. The verified root
	// document selects its exact child; a cross join cannot establish that pin.
	verified, err := s.ReadProtection(ctx, who, tenantID, binding, values[0].root)
	if err != nil {
		return zero, err
	}
	if verified.Root != values[0].root {
		return zero, v2input.ErrConflict
	}
	values[0].pixels = verified.Pixels
	return uniqueTransferTargetProtectionCandidate(values)
}

func uniqueTransferTargetProtectionCandidate(values []personCandidate) (personCandidate, error) {
	var zero personCandidate
	if len(values) == 0 {
		return zero, errTransferProtectionUnavailable
	}
	if len(values) != 1 {
		return zero, v2input.ErrConflict
	}
	out := values[0]
	if out.root.ArtifactID == uuid.Nil || out.pixels.ArtifactID == uuid.Nil || out.root.ArtifactID == out.pixels.ArtifactID {
		return zero, v2input.ErrConflict
	}
	for _, digest := range []string{out.root.SHA256, out.pixels.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	return out, nil
}

// Selects target roots only from original source-bound canonical producers.
// The later input compiler repeats physical coverage and final SQL admission
// locks every original source/person/protection proof and target inventory.
func (s SQLTransferSourceReader) selectTransferTargetProtectionConfiguration(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, base TransferPreparation) (TransferPreparation, error) {
	var zero TransferPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	base = ownTransferPreparation(base)
	if err := validateTransferPolicyConfiguration(base); err != nil {
		return zero, err
	}
	if base.PersonProtection == nil || len(base.TargetProtection) != 0 || published.Inventory.Scope.ProjectID != s.ProjectID || len(published.Inventory.Targets) < 1 || len(published.Inventory.Targets) > 5000 {
		return zero, v2input.ErrInvalid
	}
	before, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return zero, err
	}
	if !sameTransferInventory(before, published.Inventory) {
		return zero, v2input.ErrConflict
	}
	var used int64
	if err := chargeMetadata(&used, base.MetadataBytes, len(before.Targets)*512); err != nil {
		return zero, err
	}
	candidates := make([]personCandidate, len(before.Targets))
	roots := make([]v2input.Ref, len(before.Targets))
	for i, binding := range before.Targets {
		candidate, err := s.transferTargetProtectionCandidate(ctx, who, request.TenantID, binding)
		if err != nil {
			return zero, err
		}
		verified, err := s.ReadProtection(ctx, who, request.TenantID, binding, candidate.root)
		if err != nil {
			return zero, err
		}
		if verified.Pixels != candidate.pixels {
			return zero, v2input.ErrConflict
		}
		if err := chargeMetadata(&used, base.MetadataBytes, len(verified.Body)); err != nil {
			return zero, err
		}
		proof, err := s.selectPersonProtectionProof(ctx, who, request.TenantID, binding, candidate.root, base.PersonProtection.Report, base.PersonProtection.Expected)
		if err != nil {
			return zero, err
		}
		if err := chargeMetadata(&used, base.MetadataBytes, len(proof.Evidence), len(proof.Qualification), len(proof.Expected)); err != nil {
			return zero, err
		}
		current, err := s.transferTargetProtectionCandidate(ctx, who, request.TenantID, binding)
		if err != nil {
			return zero, err
		}
		if current != candidate {
			return zero, v2input.ErrConflict
		}
		candidates[i], roots[i] = candidate, verified.Root
	}
	// Later target IO must not leave earlier candidate selection stale.
	for i, binding := range before.Targets {
		current, err := s.transferTargetProtectionCandidate(ctx, who, request.TenantID, binding)
		if err != nil {
			return zero, err
		}
		if current != candidates[i] {
			return zero, v2input.ErrConflict
		}
	}
	after, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return zero, err
	}
	if !sameTransferInventory(before, after) {
		return zero, v2input.ErrConflict
	}
	base.TargetProtection = roots
	if err := validateTransferConfiguration(base); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return base, nil
}
