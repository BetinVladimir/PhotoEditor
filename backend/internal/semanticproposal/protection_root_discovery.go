package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Selects the unique canonical producer root for an already declared protected
// child. Original bounded MaskRef bytes disambiguate same-producer siblings.
// Scientific qualification and locked admission remain separate requirements.
func (s SQLTransferSourceReader) discoverProtectionRoot(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, child v2input.Ref) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || child.ArtifactID == uuid.Nil || child.ArtifactID == binding.Source.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{binding.Source.SHA256, binding.CanvasHash, child.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	var candidates []v2input.Ref
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT root.id,root.sha256
FROM photo.artifacts child
JOIN photo.artifacts root ON root.tenant_id=child.tenant_id AND root.created_by_task_id=child.created_by_task_id
WHERE child.tenant_id=$1 AND child.id=$8 AND child.sha256=$9
AND root.output_role IN('mask_protected_ref','defect_protection_ref','transformed_mask_ref','mask')
AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
AND photo.semantic_proposal_protection_origin($1,$2,$3,$4,$5,$6,$7,root.id,root.sha256,$8,$9)
ORDER BY root.id,root.sha256 LIMIT 129`, tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, child.ArtifactID, child.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var candidate v2input.Ref
			if err := rows.Scan(&candidate.ArtifactID, &candidate.SHA256); err != nil {
				return err
			}
			if candidate.ArtifactID == uuid.Nil || candidate.ArtifactID == child.ArtifactID {
				return v2input.ErrInvalid
			}
			if _, err := domain.ParseDigest(candidate.SHA256); err != nil {
				return v2input.ErrInvalid
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(candidates) == 0 {
			return v2input.ErrInvalid
		}
		if len(candidates) > 128 {
			return v2input.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	image, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return zero, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	var selected v2input.Ref
	matched, totalBytes := 0, 0
	for _, candidate := range candidates {
		object, err := catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{Ref: candidate, Contract: "MaskRef", CanvasHash: binding.CanvasHash})
		if err != nil {
			return zero, err
		}
		if object.Bytes < 2 || object.Bytes > int64((4<<20)-totalBytes) {
			return zero, v2input.ErrInvalid
		}
		body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, candidate)
		if err != nil {
			return zero, err
		}
		totalBytes += len(body)
		pixels, err := v2input.ResolveSemanticMaskPixels(candidate, body, binding, image, true)
		if err != nil {
			return zero, err
		}
		if pixels == child {
			matched++
			selected = candidate
		}
	}
	if matched == 0 {
		return zero, v2input.ErrInvalid
	}
	if matched != 1 {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return selected, nil
}

// Reads and verifies the selected original MaskRef and NPY, then repeats
// candidate discovery to reject provenance changes and newly ambiguous roots.
// Independent person coverage, policy qualification and locked admission remain
// mandatory at the caller's execution boundary.
func (s SQLTransferSourceReader) readDiscoveredProtection(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, child v2input.Ref) (VerifiedProtection, error) {
	var zero VerifiedProtection
	root, err := s.discoverProtectionRoot(ctx, who, tenantID, binding, child)
	if err != nil {
		return zero, err
	}
	verified, err := s.ReadProtection(ctx, who, tenantID, binding, root)
	if err != nil {
		return zero, err
	}
	if verified.Root != root || verified.Pixels != child {
		return zero, v2input.ErrConflict
	}
	current, err := s.discoverProtectionRoot(ctx, who, tenantID, binding, child)
	if err != nil {
		return zero, err
	}
	if current != root {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return verified, nil
}
