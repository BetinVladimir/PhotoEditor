package v2draft

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// NewSQLGlareDonorRestorationCoordinator keeps scheduler writes and editor
// catalog reads on their own roles. The caller must bind the catalogs to the
// exact source/donor asset versions selected for the draft; Lookup repeats
// current editor scope for each artifact and its immutable object version.
func NewSQLGlareDonorRestorationCoordinator(scheduler, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string, project, baseVersion,
	donorVersion uuid.UUID) (GlareDonorRestorationCoordinator, error) {
	var zero GlareDonorRestorationCoordinator
	if scheduler == nil || catalog == nil || scheduler == catalog || objects == nil ||
		len(bucket) < 3 || len(bucket) > 63 || project == uuid.Nil ||
		baseVersion == uuid.Nil || donorVersion == uuid.Nil ||
		baseVersion == donorVersion {
		return zero, v2input.ErrInvalid
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{
		DB: scheduler, Bucket: bucket}, Objects: objects}
	confirmation := v2portrait.DonorConfirmationObjectPublisher{
		Repository: v2portrait.SQLDonorConfirmationRepository{DB: scheduler,
			Bucket: bucket}, Objects: objects}
	return GlareDonorRestorationCoordinator{
		CatalogProject: project, BaseAssetVersion: baseVersion,
		DonorAssetVersion: donorVersion,
		DB:                scheduler, ConfirmationPublisher: confirmation,
		ParamsPublisher: documents, TransportPublisher: documents,
		BaseCatalog: SQLArtifactCatalog{DB: catalog, Bucket: bucket,
			ProjectID: project, AssetVersionID: baseVersion},
		DonorCatalog: SQLArtifactCatalog{DB: catalog, Bucket: bucket,
			ProjectID: project, AssetVersionID: donorVersion},
		Objects: objects,
	}, nil
}

// NewSQLGlareDonorRestorationSubmitter wires the empty-draft step to the
// scoped coordinator without granting the editor catalog any write role.
func NewSQLGlareDonorRestorationSubmitter(scheduler, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string, project, baseVersion,
	donorVersion uuid.UUID) (GlareDonorRestorationSubmitter, error) {
	coordinator, err := NewSQLGlareDonorRestorationCoordinator(scheduler,
		catalog, objects, bucket, project, baseVersion, donorVersion)
	if err != nil {
		return GlareDonorRestorationSubmitter{}, err
	}
	return GlareDonorRestorationSubmitter{
		Beginner:    SQLGlareDonorDraftBeginner{DB: scheduler},
		Coordinator: coordinator,
	}, nil
}
