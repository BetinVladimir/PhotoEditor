package workflow

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// DeliverySigningRuntime contains trusted deployment handles installed by the
// scheduler process. None of these may be inferred from a Delivery request.
type DeliverySigningRuntime struct {
	DB *pgxpool.Pool
	Reads provenance.SigningObjects
	Writes artifactjson.Objects
	SignedObjects delivery.SignedDependencyObjectReader
	Bucket string
	WorkICCSHA string
	Tenants map[uuid.UUID]DeliverySigningTenantRuntime
	Clock func() time.Time
}

type DeliverySigningTenantRuntime struct {
	Policies *provenance.SigningPolicyRegistry
	NegativePolicies *provenance.SigningNegativePolicyRegistry
	Resources delivery.DelegatedSigningResources
}

// DeliverySigningRuntimeFromDeployment transfers only live, pinned per-tenant
// handles from the shared signing deployment into the Delivery scheduler.
// The deployment owner must remain alive for the job's entire lifetime.
func DeliverySigningRuntimeFromDeployment(db *pgxpool.Pool,
	reads provenance.SigningObjects, writes artifactjson.Objects,
	signed delivery.SignedDependencyObjectReader, bucket, workICCSHA string,
	clock func() time.Time, deployment *provenance.SigningSchedulerDeployment,
) (DeliverySigningRuntime, error) {
	if deployment == nil {
		return DeliverySigningRuntime{}, v2input.ErrInvalid
	}
	handles, err := deployment.TenantHandles()
	if err != nil || len(handles) == 0 { return DeliverySigningRuntime{}, v2input.ErrInvalid }
	tenants := make(map[uuid.UUID]DeliverySigningTenantRuntime, len(handles))
	for id, installed := range handles {
		tenants[id] = DeliverySigningTenantRuntime{
			Policies: installed.Policies,
			NegativePolicies: installed.NegativePolicies,
			Resources: installed.Resources,
		}
	}
	return DeliverySigningRuntime{DB: db, Reads: reads, Writes: writes,
		SignedObjects: signed, Bucket: bucket, WorkICCSHA: workICCSHA,
		Tenants: tenants, Clock: clock}, nil
}

type deliverySigningObservations struct { db *pgxpool.Pool }

func (o deliverySigningObservations) Renew(ctx context.Context,
	lease delivery.ExpansionLease) error {
	return (delivery.ExpansionLeaseStore{DB: deliveryAssemblyObservationQueries{
		db: o.db, lease: lease}}).Renew(ctx, lease)
}

func (o deliverySigningObservations) ReadObservationPage(ctx context.Context,
	lease delivery.ExpansionLease, plan delivery.PreparePlan, cursor, limit int,
) ([]delivery.ChildObservation, error) {
	return (delivery.ExpansionLeaseStore{DB: deliveryAssemblyObservationQueries{
		db: o.db, lease: lease}}).ReadObservationPage(ctx, lease, plan, cursor, limit)
}

type tenantDeliverySigningProcessor struct { runtime DeliverySigningRuntime; pageSize int }

func (p tenantDeliverySigningProcessor) ProcessSigning(ctx context.Context,
	lease delivery.ExpansionLease, plan delivery.PreparePlan) (string, error) {
	stage, _, err := p.runtime.forTenant(lease, p.pageSize)
	if err != nil { return "", err }
	return stage.ProcessSigning(ctx, lease, plan)
}

type tenantDeliverySignedAssemblyProcessor struct {
	runtime DeliverySigningRuntime
	pipeline DeliveryAssemblyPipeline
	pageSize int
}

func (p tenantDeliverySignedAssemblyProcessor) ProcessAssembly(ctx context.Context,
	lease delivery.ExpansionLease, plan delivery.PreparePlan) (string, error) {
	_, closure, err := p.runtime.forTenant(lease, p.pageSize)
	if err != nil { return "", err }
	return (SignedDeliveryAssemblyProcessor{Pipeline: p.pipeline,
		Verifier: closure}).ProcessAssembly(ctx, lease, plan)
}

func (r DeliverySigningRuntime) forTenant(lease delivery.ExpansionLease,
	pageSize int) (DeliverySigningStage, delivery.DelegatedSigningClosureCoordinator, error) {
	var empty delivery.DelegatedSigningClosureCoordinator
	tenant, ok := r.Tenants[lease.Tenant]
	if !ok || !lease.Valid() || tenant.Policies == nil ||
		tenant.NegativePolicies == nil || tenant.Resources == nil {
		return DeliverySigningStage{}, empty, v2input.ErrInvalid
	}
	store := delivery.ExpansionLeaseStore{DB: r.DB}
	observations := deliverySigningObservations{db: r.DB}
	closure := delivery.DelegatedSigningClosureCoordinator{
		Store: store, Observations: observations, Policies: tenant.Policies,
		Clock: r.Clock, Objects: r.SignedObjects, Bucket: r.Bucket,
	}
	stage := DeliverySigningStage{
		Store: store, Observations: observations, PageSize: pageSize,
		Failure: SQLDeliveryAssemblyStore{DB: r.DB},
		SignedFailure: SQLDeliverySignedFailureStore{DB: r.DB},
		Dispatcher: delivery.DelegatedSigningSlotDispatcher{
			DB: r.DB, Reads: r.Reads, Writes: r.Writes, Bucket: r.Bucket,
			WorkICCSHA: r.WorkICCSHA, Policies: tenant.Policies,
			NegativePolicies: tenant.NegativePolicies,
			Resources: tenant.Resources, Clock: r.Clock,
		}, Closure: closure,
	}
	return stage, closure, nil
}

// InstallDeliverySigningStage installs both required-policy branches. A
// partially configured signer never replaces the existing off-policy path.
func InstallDeliverySigningStage(job DeliveryExpansionJob,
	runtime DeliverySigningRuntime) (DeliveryExpansionJob, error) {
	if runtime.DB == nil || runtime.Reads == nil || runtime.Writes == nil ||
		runtime.SignedObjects == nil || len(runtime.Tenants) == 0 ||
		runtime.Clock == nil || len(runtime.Bucket) < 3 ||
		len(runtime.Bucket) > 63 || domainDigestInvalid(runtime.WorkICCSHA) ||
		job.Driver.Store == nil || job.Driver.Assembly == nil ||
		job.Driver.PageSize < 1 || job.Driver.PageSize > 100 ||
		job.Driver.Signing != nil || job.Driver.SignedAssembly != nil {
		return DeliveryExpansionJob{}, v2input.ErrInvalid
	}
	assembly, ok := job.Driver.Assembly.(DeliveryAssemblyPipeline)
	if !ok || assembly.DB != runtime.DB || assembly.Bucket != runtime.Bucket {
		return DeliveryExpansionJob{}, v2input.ErrInvalid
	}
	assembly.Objects = runtime.Writes
	assembly.SignedObjects = runtime.SignedObjects
	ownedTenants := make(map[uuid.UUID]DeliverySigningTenantRuntime, len(runtime.Tenants))
	for id, tenant := range runtime.Tenants {
		if id == uuid.Nil || tenant.Policies == nil ||
			tenant.NegativePolicies == nil || tenant.Resources == nil {
			return DeliveryExpansionJob{}, v2input.ErrInvalid
		}
		projected, err := tenant.Policies.ForTenant(id)
		if err != nil { return DeliveryExpansionJob{}, err }
		tenant.Policies = projected
		ownedTenants[id] = tenant
	}
	runtime.Tenants = ownedTenants
	job.Driver.Signing = tenantDeliverySigningProcessor{runtime: runtime,
		pageSize: job.Driver.PageSize}
	job.Driver.SignedAssembly = tenantDeliverySignedAssemblyProcessor{
		runtime: runtime, pipeline: assembly, pageSize: job.Driver.PageSize}
	return job, nil
}
