package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// A server-owned factory binds each restart to its original reference version.
// The returned catalog is a lookup dependency, never admission authority.
type NativeParentArtifactCatalogFactory func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) (v2draft.ArtifactCatalog, error)

type SQLNativeParentArtifactCatalogFactory struct {
	API    *pgxpool.Pool
	Bucket string
}

func (f SQLNativeParentArtifactCatalogFactory) ForReference(ctx context.Context, who auth.Identity, tenantID, project, version uuid.UUID) (v2draft.ArtifactCatalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.API == nil || f.Bucket == "" || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil || project == uuid.Nil || version == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	var role string
	if err := f.API.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return nil, err
	}
	if role != "photo_api" {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Each Lookup independently repeats editor scope and exact version provenance.
	return v2draft.SQLArtifactCatalog{DB: f.API, Bucket: f.Bucket, ProjectID: project, AssetVersionID: version}, nil
}

func resolveNativeParentRecoveryCatalog(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, project uuid.UUID, configured v2draft.ArtifactCatalog, factory NativeParentArtifactCatalogFactory) (v2draft.ArtifactCatalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request := anchor.Request
	if request.TenantID == uuid.Nil || request.RunID == uuid.Nil || request.ActorID != who.UserID || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || project == uuid.Nil || anchor.Published.Inventory.Scope.ProjectID != project || request.Intent.Reference.AssetVersionID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	if factory == nil {
		if configured == nil {
			return nil, v2input.ErrInvalid
		}
		return configured, nil
	}
	catalog, err := factory(ctx, who, request.TenantID, project, request.Intent.Reference.AssetVersionID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, v2input.ErrInvalid
	}
	return catalog, nil
}
