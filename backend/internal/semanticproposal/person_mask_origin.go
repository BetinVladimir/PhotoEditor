package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// VerifyPersonMaskOrigin proves mask.person canonical producer lineage before run creation.
// It does not qualify the model, select policy coverage or validate MaskRef/NPY bytes.
func (s SQLTransferSourceReader) VerifyPersonMaskOrigin(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, root, child v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || root.ArtifactID == uuid.Nil || child.ArtifactID == uuid.Nil {
		return v2input.ErrInvalid
	}
	return tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_person_mask_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, root.ArtifactID, root.SHA256, child.ArtifactID, child.SHA256).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2input.ErrInvalid
		}
		return nil
	})
}
