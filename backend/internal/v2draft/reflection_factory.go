package v2draft

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// NewSQLReflectionRestorationCoordinator separates the scheduler's write
// authority from the version-scoped editor catalogue and immutable object IO.
func NewSQLReflectionRestorationCoordinator(scheduler, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string, project, baseVersion uuid.UUID) (
	ReflectionRestorationCoordinator, error) {
	var zero ReflectionRestorationCoordinator
	if scheduler == nil || catalog == nil || scheduler == catalog ||
		objects == nil || len(bucket) < 3 || len(bucket) > 63 ||
		project == uuid.Nil || baseVersion == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{
		DB: scheduler, Bucket: bucket}, Objects: objects}
	return ReflectionRestorationCoordinator{CatalogProject: project,
		BaseAssetVersion: baseVersion, DB: scheduler,
		ParamsPublisher: documents, Transport: documents,
		BaseCatalog: SQLArtifactCatalog{DB: catalog, Bucket: bucket,
			ProjectID: project, AssetVersionID: baseVersion},
		Objects: objects}, nil
}

func NewSQLReflectionRestorationSubmitter(scheduler, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string, project, baseVersion uuid.UUID) (
	ReflectionRestorationSubmitter, error) {
	coordinator, err := NewSQLReflectionRestorationCoordinator(scheduler, catalog,
		objects, bucket, project, baseVersion)
	if err != nil {
		return ReflectionRestorationSubmitter{}, err
	}
	return ReflectionRestorationSubmitter{Beginner: SQLGlareDonorDraftBeginner{DB: scheduler},
		Coordinator: coordinator}, nil
}
