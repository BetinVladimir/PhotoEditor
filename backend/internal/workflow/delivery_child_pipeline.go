package workflow

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// DeliveryResolvedChild contains exact versioned source bytes and a complete
// authorized typed transitive inventory. ResolveChild must verify SQL authority,
// physical SHA/canonical JSON and the ICC pin; it must not accept client refs.
type DeliveryResolvedChild struct {
	ImageJCS     []byte
	TargetICCSHA string
	Entries      []v2input.Entry
}
type DeliveryChildResolver interface {
	ResolveChild(context.Context, delivery.ExpansionLease, delivery.PreparePlan, delivery.ExportChildIntent) (DeliveryResolvedChild, error)
}

// DeliveryChildPipeline requires a concrete authorized resolver. It has no
// permissive fallback and is not installed by merely constructing this type.
// All source/object IO is outside the child admission transactions.
type DeliveryChildPipeline struct {
	DB       *pgxpool.Pool
	Objects  artifactjson.Objects
	Reader   v2input.SessionAnalysisObjectReader
	Bucket   string
	Resolver DeliveryChildResolver
	Registry Registry
	Limits   DeliveryDerivativeLimits
}

func (p DeliveryChildPipeline) ProcessChild(ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent) (DeliveryChildAdmission, error) {
	if err := ctx.Err(); err != nil {
		return DeliveryChildAdmission{}, err
	}
	if p.DB == nil || p.Objects == nil || p.Resolver == nil || len(p.Bucket) < 3 || len(p.Bucket) > 63 {
		return DeliveryChildAdmission{}, v2input.ErrInvalid
	}
	if _, _, err := plan.ExportRequest(lease.Run, child); err != nil {
		return DeliveryChildAdmission{}, err
	}
	source, err := p.Resolver.ResolveChild(ctx, lease, plan, child)
	if err != nil {
		return DeliveryChildAdmission{}, err
	}
	// Own the resolver's result across publication/compilation stages.
	source.ImageJCS = append([]byte(nil), source.ImageJCS...)
	source.Entries = append([]v2input.Entry(nil), source.Entries...)
	// Refuse impossible CPU/scratch/output budgets before reserving or writing
	// immutable Params. Freeze below repeats this check for the final closure.
	node, err := CompileDeliveryInitialNode(plan, lease.Run, child, source.ImageJCS, source.TargetICCSHA, p.Registry)
	if err != nil { return DeliveryChildAdmission{}, err }
	if _, err := compileDeliveryInitialProfile(node, source.ImageJCS, p.Limits); err != nil {
		return DeliveryChildAdmission{}, err
	}
	if err := ctx.Err(); err != nil { return DeliveryChildAdmission{}, err }
	params, err := PublishDeliveryInitialParams(ctx, p.DB, p.Objects, p.Bucket, lease, plan, child, source.ImageJCS, source.TargetICCSHA, p.Registry)
	if err != nil {
		return DeliveryChildAdmission{}, err
	}
	frozen, err := FreezeDeliveryInitialTask(plan, lease.Run, child, source.ImageJCS, source.TargetICCSHA, p.Registry, params, source.Entries, p.Limits)
	if err != nil {
		return DeliveryChildAdmission{}, err
	}
	policy, err := plan.MetadataPolicy()
	if err != nil { return DeliveryChildAdmission{}, err }
	if policy.MetadataPolicy != "strip" {
		store, err := delivery.NewMetadataInventoryEntryStore(p.DB, lease.Tenant)
		if err != nil { return DeliveryChildAdmission{}, err }
		frozen, err = frozen.WithMetadataConfig(ctx, store, lease, plan, child)
		if err != nil { return DeliveryChildAdmission{}, err }
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: p.DB, Bucket: p.Bucket}, Objects: p.Objects}
	transport := v2input.ScopedPublisher{Documents: documents, Tenant: lease.Tenant, Run: child.RunID}
	return frozen.AdmitNewChild(ctx, SQLDeliveryChildStore{DB: p.DB}, transport, lease, plan, child)
}
