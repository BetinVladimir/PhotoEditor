package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type CatalogInventory struct {
	Scope   CatalogScope
	Targets []v2input.SessionVersionBinding
}

// ReadInventory returns complete catalog bindings in snapshot ordinal order.
// Object documents, canvas bytes and admission locks still need verification.
func ReadInventory(ctx context.Context, db *pgxpool.Pool, who auth.Identity, request Request) (CatalogInventory, error) {
	var out CatalogInventory
	if db == nil || request.TenantID == uuid.Nil || who.UserID == uuid.Nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil {
		return out, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(request.Intent)
	if err != nil {
		return out, err
	}
	input, err := v2input.DecodeSemanticEditIntent(body)
	if err != nil {
		return out, err
	}
	err = tenant.WithTenant(ctx, db, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT project_id,item_count,ordinal,asset_version_id,source_id,source_sha,canvas_hash FROM photo.semantic_proposal_inventory($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", request.TenantID, who.UserID, who.SessionID, input.Reference.AssetVersionID, input.Reference.Source.ArtifactID, input.Reference.Source.SHA256, input.Reference.CanvasHash, input.ReferenceMask.ArtifactID, input.ReferenceMask.SHA256, input.TargetSnapshotID)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[uuid.UUID]bool{}
		for rows.Next() {
			var scope CatalogScope
			var ordinal int
			var binding v2input.SessionVersionBinding
			if err := rows.Scan(&scope.ProjectID, &scope.ItemCount, &ordinal, &binding.AssetVersionID, &binding.Source.ArtifactID, &binding.Source.SHA256, &binding.CanvasHash); err != nil {
				return err
			}
			if len(out.Targets) == 0 {
				out.Scope = scope
			}
			_, sourceErr := domain.ParseDigest(binding.Source.SHA256)
			_, canvasErr := domain.ParseDigest(binding.CanvasHash)
			if scope != out.Scope || scope.ProjectID == uuid.Nil || scope.ItemCount < 1 || scope.ItemCount > 5000 || ordinal != len(out.Targets) || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || seen[binding.AssetVersionID] || sourceErr != nil || canvasErr != nil {
				return v2input.ErrInvalid
			}
			seen[binding.AssetVersionID] = true
			out.Targets = append(out.Targets, binding)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out.Targets) == 0 {
			return domain.NewError("NOT_FOUND", 404)
		}
		if len(out.Targets) != out.Scope.ItemCount {
			return v2input.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return CatalogInventory{}, err
	}
	return out, nil
}
