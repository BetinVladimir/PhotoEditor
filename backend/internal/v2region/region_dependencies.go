package v2region

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type RegionQueueDependencies struct {
	Resolver RegionInputResolver
	Params   RegionParamsPublisher
	Closure  v2input.Publisher
}
type RegionDependencyFactory interface {
	ForRegionPlan(context.Context, FrozenRegionPlan, uuid.UUID) (RegionQueueDependencies, error)
}

// SQLRegionDependencies makes fresh scope-bound catalogs/publishers for each
// frozen plan. No request's tenant/project/version/run leaks into another one.
type SQLRegionDependencies struct {
	API, Scheduler *pgxpool.Pool
	Objects        *objectstore.Store
	Bucket         string
}

func (f SQLRegionDependencies) ForRegionPlan(ctx context.Context, plan FrozenRegionPlan, run uuid.UUID) (RegionQueueDependencies, error) {
	if err := ctx.Err(); err != nil {
		return RegionQueueDependencies{}, err
	}
	if f.API == nil || f.Scheduler == nil || f.API == f.Scheduler || f.Objects == nil || f.Bucket == "" || run == uuid.Nil {
		return RegionQueueDependencies{}, ErrRegionUnavailable
	}
	if _, err := plan.SourceShape(); err != nil {
		return RegionQueueDependencies{}, err
	}
	doc, err := plan.decode()
	if err != nil || doc.TenantID == uuid.Nil || doc.ProjectID == uuid.Nil || doc.Request.VersionID == uuid.Nil {
		return RegionQueueDependencies{}, ErrRegionInvalid
	}
	var resolver RegionInputResolver = v2draft.ClosureResolver{Catalog: v2draft.SQLArtifactCatalog{DB: f.API, Bucket: f.Bucket, ProjectID: doc.ProjectID, AssetVersionID: doc.Request.VersionID}, Objects: f.Objects}
 if doc.Execution!=nil{
  resolver=regionExecutionResolver{base:resolver,plan:plan,catalog:regionExecutionCatalog{API:f.API,Objects:f.Objects,Bucket:f.Bucket,Tenant:doc.TenantID,Asset:doc.AssetID,Version:doc.Request.VersionID,Source:doc.Source.Ref,Style:doc.FrozenRecipe.BaseStyle}}
 }

	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: f.Scheduler, Bucket: f.Bucket}, Objects: f.Objects}
	return RegionQueueDependencies{Resolver: resolver, Params: v2draft.Publisher{DB: f.API, Objects: f.Objects, Bucket: f.Bucket},
		Closure: v2input.ScopedPublisher{Documents: documents, Tenant: doc.TenantID, Run: run}}, nil
}
