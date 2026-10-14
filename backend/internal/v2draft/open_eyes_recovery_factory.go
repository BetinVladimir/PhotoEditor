package v2draft

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"photoeditor/backend/internal/v2qualification"
)

type OpenEyesRecoveryDependencies struct {
	Loader    DonorRecoveryLoader
	Params    SQLDonorParamsPublisher
	Documents v2input.DocumentPublisher
	Scheduler *pgxpool.Pool
	Objects   *objectstore.Store
	Bucket    string
	Project   uuid.UUID
	Base      uuid.UUID
	Donor     uuid.UUID
}

// NewSQLOpenEyesRecoveryDependencies keeps scheduler writes and editor-scoped
// catalog reads separate. Operator locks and dimensions are fixed at install;
// current qualification and exact source versions are rechecked during reload.
func NewSQLOpenEyesRecoveryDependencies(scheduler, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string, project, baseVersion, donorVersion uuid.UUID,
	locks v2qualification.DeploymentLocks, patchWidth, patchHeight int64,
	capacity v2portrait.DonorResourceEstimate) (OpenEyesRecoveryDependencies, error) {
	var zero OpenEyesRecoveryDependencies
	if scheduler == nil || catalog == nil || scheduler == catalog || objects == nil ||
		len(bucket) < 3 || len(bucket) > 63 || project == uuid.Nil ||
		baseVersion == uuid.Nil || donorVersion == uuid.Nil || baseVersion == donorVersion ||
		patchWidth < 1 || patchHeight < 1 || patchWidth > 1048576 ||
		patchHeight > 1048576/patchWidth || capacity.OutputBytes < 1 {
		return zero, v2input.ErrInvalid
	}
	if locks.Operation != "portrait.open_eyes" || locks.Version != "1" || locks.Backend != "cpu" || !locks.LicenseCleared {
		return zero, v2input.ErrInvalid
	}
	if _, err := locks.Expected(); err != nil {
		return zero, err
	}
	base := SQLArtifactCatalog{DB: catalog, Bucket: bucket, ProjectID: project,
		AssetVersionID: baseVersion}
	donor := SQLArtifactCatalog{DB: catalog, Bucket: bucket, ProjectID: project,
		AssetVersionID: donorVersion}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{
		DB: scheduler, Bucket: bucket}, Objects: objects}
	loader := DonorRecoveryLoader{Source: ClosureResolver{Catalog: base, Objects: objects},
		DonorCatalog: donor, QualificationCatalog: v2portrait.SQLDonorQualificationCatalog{
			DB: scheduler, Bucket: bucket}, Locks: locks,
		PatchWidth: patchWidth, PatchHeight: patchHeight, Capacity: capacity}
	return OpenEyesRecoveryDependencies{Loader: loader,
		Params:    SQLDonorParamsPublisher{DB: scheduler, Publisher: documents},
		Documents: documents, Scheduler: scheduler, Objects: objects, Bucket: bucket,
		Project: project, Base: baseVersion, Donor: donorVersion}, nil
}

// LoadManualProducer obtains both source and donor manifests under independent
// version scopes, then reads the exact manual root/Params/vectors lineage. The
// returned bytes are still revalidated by donor recovery and final SQL admit.
func (s OpenEyesRecoveryDependencies) LoadManualProducer(ctx context.Context,
	who auth.Identity, tenant, snapshot uuid.UUID, intent OpenEyesClientIntent,
	issued v2portrait.OpenEyesParams) (DonorManualProducer, error) {
	var zero DonorManualProducer
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Scheduler == nil || s.Objects == nil || s.Loader.Source.Catalog == nil ||
		s.Loader.DonorCatalog == nil || s.Project == uuid.Nil ||
		tenant == uuid.Nil || snapshot == uuid.Nil || snapshot != intent.Snapshot ||
		issued.Base.AssetVersionID != s.Base || issued.Donor.AssetVersionID != s.Donor ||
		issued.Base != intent.Proposal.Base || issued.Donor != intent.Proposal.Donor ||
		issued.EyesMask != intent.Proposal.EyesMask || issued.Protected != intent.Proposal.Protected {
		return zero, v2input.ErrInvalid
	}
	images, err := LoadDonorImageObjects(ctx, who, tenant, issued,
		s.Objects, s.Loader.Source.Catalog, s.Loader.DonorCatalog)
	if err != nil {
		return zero, err
	}
	documents, err := v2portrait.ReadManualPairDocuments(ctx, s.Scheduler,
		s.Objects, who, tenant, s.Project, snapshot, issued.Base, images.SourceRaw,
		issued.EyesMask, issued.Protected, intent.TargetPixel, intent.ProtectedPixel,
		s.Bucket)
	if err != nil {
		return zero, err
	}
	return DonorManualProducer{RootRef: documents.Origin.Root,
		RootRaw:    append([]byte(nil), documents.RootRaw...),
		ParamsRef:  documents.Origin.Params,
		ParamsRaw:  append([]byte(nil), documents.ParamsRaw...),
		VectorsRaw: appendOptionalBytes(documents.VectorsRaw)}, nil
}
