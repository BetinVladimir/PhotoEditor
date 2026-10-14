package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type CatalogScope struct {
	ProjectID uuid.UUID
	ItemCount int
}

// ReadCatalog checks live editor authority and ready source/snapshot/user mask
// inventory without object IO. It cannot authorize subsequent admission alone.
func ReadCatalog(ctx context.Context, db *pgxpool.Pool, who auth.Identity, request Request) (CatalogScope, error) {
	var scope CatalogScope
	if db == nil || who.UserID != request.ActorID || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || request.TenantID == uuid.Nil {
		return scope, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(request.Intent)
	if err != nil {
		return scope, err
	}
	input, err := v2input.DecodeSemanticEditIntent(body)
	if err != nil {
		return scope, err
	}
	err = tenant.WithTenant(ctx, db, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		err := tx.QueryRow(ctx, "SELECT project_id,item_count FROM photo.semantic_proposal_catalog($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", request.TenantID, who.UserID, who.SessionID, input.Reference.AssetVersionID, input.Reference.Source.ArtifactID, input.Reference.Source.SHA256, input.Reference.CanvasHash, input.ReferenceMask.ArtifactID, input.ReferenceMask.SHA256, input.TargetSnapshotID).Scan(&scope.ProjectID, &scope.ItemCount)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if scope.ProjectID == uuid.Nil || scope.ItemCount < 1 || scope.ItemCount > 5000 {
			return v2input.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return CatalogScope{}, err
	}
	return scope, nil
}
