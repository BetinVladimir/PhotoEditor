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

// DefectOriginPins are derived from validated original documents and catalog.
// This structure alone does not prove document bytes or scientific qualification.
type DefectOriginPins struct{ Proposal, Report, Labels, Protection, Params v2input.Ref }

func (s SQLTransferSourceReader) VerifyDefectOrigin(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil {
		return v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{pins.Proposal, pins.Report, pins.Labels, pins.Protection, pins.Params} {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	return tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_defect_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2input.ErrInvalid
		}
		return nil
	})
}
