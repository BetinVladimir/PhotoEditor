package v2draft

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

// A factory binds a fresh resolver to the saved owner; no mutable catalog is
// shared across concurrent runs with different project/version scopes.
type LocalPortraitRecoveryFactory interface {
	Build(context.Context, LocalPortraitOwnerRequest) (LocalPortraitMaterialLoader, LocalPortraitCoordinator, error)
}

type SQLLocalPortraitRecoveryFactory struct {
	// DB uses photo_scheduler for durable owner and producer lineage.
	DB *pgxpool.Pool
	// CatalogDB uses photo_api for authenticated editor artifact scope.
	CatalogDB *pgxpool.Pool
	Bucket    string
	Objects   v2input.SessionAnalysisObjectReader
	Documents v2input.DocumentPublisher
}

func (f SQLLocalPortraitRecoveryFactory) Build(ctx context.Context, request LocalPortraitOwnerRequest) (LocalPortraitMaterialLoader, LocalPortraitCoordinator, error) {
	if err := ctx.Err(); err != nil {
		return nil, LocalPortraitCoordinator{}, err
	}
	if f.DB == nil || f.CatalogDB == nil || f.Bucket == "" || f.Objects == nil || f.Documents == nil || request.SchemaVersion != 1 || request.Tenant == uuid.Nil || request.Project == uuid.Nil || request.Intent.Binding.AssetVersionID == uuid.Nil {
		return nil, LocalPortraitCoordinator{}, v2input.ErrInvalid
	}
	resolver := ClosureResolver{Catalog: SQLArtifactCatalog{DB: f.CatalogDB, Bucket: f.Bucket, ProjectID: request.Project, AssetVersionID: request.Intent.Binding.AssetVersionID}, Objects: f.Objects}
	material := SQLLocalPortraitMaterialLoader{DB: f.DB, Bucket: f.Bucket, Resolver: resolver}
	coordinator := LocalPortraitCoordinator{Resolver: resolver, Owners: SQLLocalPortraitOwnerScope{DB: f.DB}, Documents: f.Documents}
	return material, coordinator, nil
}

var _ LocalPortraitRecoveryFactory = SQLLocalPortraitRecoveryFactory{}
