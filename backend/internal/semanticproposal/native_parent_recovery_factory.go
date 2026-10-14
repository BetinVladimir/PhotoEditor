package semanticproposal

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Deployment capacities are supplied by the server. Qualification and mask refs
// are selected separately for each original tenant/request, never by this factory.
// Shared pools/storage remain owned by the calling service.
type SQLNativeParentRecoveryFactory struct {
	API, Scheduler                  *pgxpool.Pool
	Objects                         *objectstore.Store
	Bucket                          string
	Capacity                        TransferPreparation
	ReservedBytes                   int64
	ManualLimits, ManualBatchLimits ManualRasterVerificationLimits
	Timeout                         time.Duration
}

func (f SQLNativeParentRecoveryFactory) validateCapacity() error {
	if err := validateTransferCapacityConfiguration(f.Capacity); err != nil {
		return err
	}
	if f.ReservedBytes <= 0 || f.ReservedBytes > 9007199254740991 || f.Timeout <= 0 || f.Timeout > 29*time.Minute {
		return v2input.ErrInvalid
	}
	for _, limits := range []ManualRasterVerificationLimits{f.ManualLimits, f.ManualBatchLimits} {
		for _, value := range []int64{limits.PointChecks, limits.EdgeEvaluations, limits.PayloadBytes} {
			if value <= 0 || value > 9007199254740991 {
				return v2input.ErrInvalid
			}
		}
	}
	return nil
}

func (f SQLNativeParentRecoveryFactory) ForProject(ctx context.Context, project uuid.UUID) (SQLNativeManualParentRecovery, error) {
	var zero SQLNativeManualParentRecovery
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := f.validateCapacity(); err != nil {
		return zero, err
	}
	if f.API == nil || f.Scheduler == nil || f.API == f.Scheduler || f.Scheduler.Config().MaxConns < 2 || f.Objects == nil || len(f.Bucket) < 3 || len(f.Bucket) > 63 || project == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, check := range []struct {
		pool *pgxpool.Pool
		role string
	}{{f.API, "photo_api"}, {f.Scheduler, "photo_scheduler"}} {
		var role string
		if err := check.pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return zero, err
		}
		if role != check.role {
			return zero, v2input.ErrInvalid
		}
	}
	reader := SQLTransferSourceReader{DB: f.API, Objects: f.Objects, Bucket: f.Bucket, ProjectID: project, ManualRasterLimits: f.ManualLimits, ManualRasterBatchLimits: f.ManualBatchLimits}
	documents := v2draft.Publisher{DB: f.API, Objects: f.Objects, Bucket: f.Bucket}
	transport := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: f.Scheduler, Bucket: f.Bucket}, Objects: f.Objects}
	preparer := TransferDraftPreparer{Reader: reader, Documents: documents, Scheduler: f.Scheduler, TransportDocuments: transport, Configuration: ownTransferPreparation(f.Capacity), ReservedBytes: f.ReservedBytes}
	catalogs := SQLNativeParentArtifactCatalogFactory{API: f.API, Bucket: f.Bucket}
	job := SQLNativeManualParentRecovery{Preparer: preparer, CatalogForReference: catalogs.ForReference, DiscoverPersonPolicy: true, DiscoverTransferPolicy: true, Timeout: f.Timeout}
	if err := job.validate(ctx); err != nil {
		return zero, err
	}
	return job, nil
}

func (f SQLNativeParentRecoveryFactory) InitialForProject(ctx context.Context, project uuid.UUID) (SQLNativeInitialPipelineRecovery, error) {
	original, err := f.ForProject(ctx, project)
	if err != nil {
		return SQLNativeInitialPipelineRecovery{}, err
	}
	job := SQLNativeInitialPipelineRecovery{Preparer: original.Preparer, CatalogForReference: original.CatalogForReference, Timeout: f.Timeout}
	if err := job.validate(ctx); err != nil {
		return SQLNativeInitialPipelineRecovery{}, err
	}
	return job, nil
}
