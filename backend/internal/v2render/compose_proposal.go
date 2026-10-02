package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// CheckLensProposal proves exact adopted lens root provenance for the requested
// base source/version. It does not prove photometric geometry, mask lineage or
// task admission; pinned JSON bytes must still pass VerifyCompositionParents.
func (s SQLOriginalCompositionPreflight) CheckLensProposal(ctx context.Context, tenant, project uuid.UUID, who auth.Identity, params LayerComposeParams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || params.Base.AssetVersionID == uuid.Nil || !validLayerArtifactRef(params.Base.Source) || !validLayerArtifactRef(params.ProposalResult) || !validLayerArtifactRef(v2input.Ref{ArtifactID: params.Base.AssetVersionID, SHA256: params.Base.CanvasHash}) {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_lens_proposal_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenant, project, params.Base.AssetVersionID, who.UserID, who.SessionID, params.ProposalResult.ArtifactID, params.ProposalResult.SHA256, params.Base.Source.ArtifactID, params.Base.Source.SHA256, params.Base.CanvasHash).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}

// CheckLensMask accepts only an exact adopted changed/generated mask document
// from the proved lens proposal. Its pinned JSON/binary geometry and values must
// still be verified; user masks and other producers are not authorized here.
func (s SQLOriginalCompositionPreflight) CheckLensMask(ctx context.Context, tenant, project uuid.UUID, who auth.Identity, params LayerComposeParams, mask v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validLayerArtifactRef(mask) {
		return v2input.ErrInvalid
	}
	if err := s.CheckLensProposal(ctx, tenant, project, who, params); err != nil {
		return err
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_lens_mask_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tenant, project, params.Base.AssetVersionID, who.UserID, who.SessionID, params.ProposalResult.ArtifactID, params.ProposalResult.SHA256, params.Base.Source.ArtifactID, params.Base.Source.SHA256, params.Base.CanvasHash, mask.ArtifactID, mask.SHA256).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}
