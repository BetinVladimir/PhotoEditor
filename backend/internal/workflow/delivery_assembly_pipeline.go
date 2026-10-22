package workflow

import (
	"context"
	"github.com/google/uuid"
 "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type DeliveryAssemblyRegistrationStore interface {
	Registered(context.Context, FrozenDeliveryAssemblyTask, delivery.ExpansionLease, delivery.PreparePlan) (bool, error)
	Create(context.Context, FrozenDeliveryAssemblyTask, delivery.ExpansionLease, delivery.PreparePlan) error
	Renew(context.Context, delivery.ExpansionLease) error
	Register(context.Context, FrozenDeliveryAssemblyTask, delivery.ExpansionLease, delivery.PreparePlan, v2input.GraphTask) error
}

// RegisterPreparedAssembly commits the blocked shell before transport object IO.
// Exact sealed retries skip publication; registration errors leave replayable
// blocked state. A returned task is registered, not activated or dispatched.
func (f FrozenDeliveryAssemblyTask) RegisterPreparedAssembly(ctx context.Context, store DeliveryAssemblyRegistrationStore, publisher v2input.Publisher, lease delivery.ExpansionLease, plan delivery.PreparePlan) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if store == nil || publisher == nil || !lease.Valid() || lease.ParentTask != plan.TaskID() {
		return uuid.Nil, v2input.ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return uuid.Nil, err
	}
	owner, err := v2input.TaskID(lease.Workflow, "v2.delivery.assemble", 0)
	if err != nil || owner != draft.ID {
		return uuid.Nil, v2input.ErrInvalid
	}
	ready, err := store.Registered(ctx, f, lease, plan)
	if err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil {return uuid.Nil, err}
	if ready {
		return draft.ID, nil
	}
	if err := store.Create(ctx, f, lease, plan); err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil {return uuid.Nil, err}
	if err := store.Renew(ctx, lease); err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil {return uuid.Nil, err}
	graph, err := f.PublishInputs(ctx, publisher)
	if err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil {return uuid.Nil, err}
	if err := store.Renew(ctx, lease); err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil {return uuid.Nil, err}
	if err := store.Register(ctx, f, lease, plan, graph); err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil {return uuid.Nil, err}
	return draft.ID, nil
}

type SQLDeliveryAssemblyStore struct{ DB *pgxpool.Pool }

func (s SQLDeliveryAssemblyStore) Registered(ctx context.Context, f FrozenDeliveryAssemblyTask, l delivery.ExpansionLease, p delivery.PreparePlan) (bool, error) {
	if s.DB == nil {
		return false, v2input.ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return false, err
	}
	var ready bool
 err = assemblyTenantTransaction(ctx,s.DB,l,func(ctx context.Context,tx pgx.Tx)error{
  var readErr error
  ready,readErr=(delivery.ExpansionLeaseStore{DB:tx}).AssemblyTransportReady(ctx,l,p,draft,f.task.closure.ParamsJCS)
  return readErr
 })
 return ready,err
}
func (s SQLDeliveryAssemblyStore) Renew(ctx context.Context, l delivery.ExpansionLease) error {
	if s.DB == nil {
		return v2input.ErrInvalid
	}
	return (delivery.ExpansionLeaseStore{DB: deliveryAssemblyObservationQueries{db:s.DB,lease:l}}).Renew(ctx, l)
}
func (s SQLDeliveryAssemblyStore) Create(ctx context.Context, f FrozenDeliveryAssemblyTask, l delivery.ExpansionLease, p delivery.PreparePlan) error {
	if s.DB == nil {
		return v2input.ErrInvalid
	}
 return assemblyTenantTransaction(ctx,s.DB,l,func(ctx context.Context,tx pgx.Tx)error{
  return f.CreateInTransaction(ctx,tx,l,p)
 })
}
func (s SQLDeliveryAssemblyStore) Register(ctx context.Context, f FrozenDeliveryAssemblyTask, l delivery.ExpansionLease, p delivery.PreparePlan, g v2input.GraphTask) error {
	if s.DB == nil {
		return v2input.ErrInvalid
	}
 return assemblyTenantTransaction(ctx,s.DB,l,func(ctx context.Context,tx pgx.Tx)error{
  return f.RegisterInputsInTransaction(ctx,tx,l,p,g)
 })
}

type DeliveryAssemblyPipeline struct {
	DB      *pgxpool.Pool
	Objects artifactjson.Objects
	SignedObjects delivery.SignedDependencyObjectReader
	Bucket  string
	Limits  DeliveryDerivativeLimits
}

// PrepareAndRegister performs source recovery and publication outside the two
// short admission transactions. Scheduler installation/activation is separate.
func (p DeliveryAssemblyPipeline) PrepareAndRegister(ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan, root v2input.Ref) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if p.DB == nil || p.Objects == nil || len(p.Bucket) < 3 || len(p.Bucket) > 63 {
		return uuid.Nil, v2input.ErrInvalid
	}
	frozen, err := p.prepare(ctx, lease, plan, root)
	if err != nil {
		return uuid.Nil, err
	}
	return p.register(ctx, frozen, lease, plan)

}

// Activate acknowledges a previous commit before attempting the first transition.
// Errors, including uncertain commit outcomes, leave the descriptor retryable.
func (s SQLDeliveryAssemblyStore) Activate(ctx context.Context, f FrozenDeliveryAssemblyTask, l delivery.ExpansionLease, p delivery.PreparePlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil {
		return v2input.ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return err
	}
 return assemblyTenantTransaction(ctx,s.DB,l,func(ctx context.Context,tx pgx.Tx)error{
  store:=delivery.ExpansionLeaseStore{DB:tx}
  acknowledged,err:=store.AssemblyActivated(ctx,l,p,draft,f.task.closure.ParamsJCS)
  if err!=nil{return err}
  if acknowledged{return nil}
  return store.ActivateAssembly(ctx,l,p,draft,f.task.closure.ParamsJCS)
 })
}

func (p DeliveryAssemblyPipeline) prepare(ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan, root v2input.Ref) (FrozenDeliveryAssemblyTask, error) {
	recovery := deliveryAssemblyRecoveryQueries{deliveryAssemblyObservationQueries:deliveryAssemblyObservationQueries{db:p.DB,lease:lease},root:root}
	return prepareDeliveryAssemblyBeforePublication(ctx, delivery.ExpansionLeaseStore{DB: recovery}, lease, plan, root, p.Limits,
		func() (v2input.Ref, error) {
			params, _, err := PublishDeliveryAssemblyParams(ctx, deliveryAssemblyPublicationQueries{db:p.DB,lease:lease}, p.Objects, p.Bucket, lease, plan, root)
			return params, err
		})
}
func (p DeliveryAssemblyPipeline) register(ctx context.Context, frozen FrozenDeliveryAssemblyTask, lease delivery.ExpansionLease, plan delivery.PreparePlan) (uuid.UUID, error) {
	draft,err:=frozen.DraftTask()
 if err!=nil{return uuid.Nil,err}
 transport:=deliveryAssemblyTransportQueries{db:p.DB,lease:lease,task:draft.ID}
 documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: transport, Bucket: p.Bucket}, Objects: p.Objects}
	publisher := v2input.ScopedPublisher{Documents: documents, Tenant: lease.Tenant, Run: lease.Run}
	return frozen.RegisterPreparedAssembly(ctx, SQLDeliveryAssemblyStore{DB: p.DB}, publisher, lease, plan)
}

// ProcessAssembly observes the complete current matrix, publishes retained inputs,
// registers the task and commits activation. Pending/failed matrices publish no root.
func (p DeliveryAssemblyPipeline) ProcessAssembly(ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if p.DB == nil || p.Objects == nil || len(p.Bucket) < 3 || len(p.Bucket) > 63 {
		return "", v2input.ErrInvalid
	}
	matrix := delivery.ExpansionLeaseStore{DB: deliveryAssemblyObservationQueries{db:p.DB,lease:lease}}
	root, state, err := PublishDeliveryAssemblyInput(ctx, deliveryAssemblyPublicationQueries{db:p.DB,lease:lease}, p.Objects, p.Bucket, matrix, lease, plan)
	if err != nil {
		return "", err
	}
	if state == "failed" {
		finalized, err := FinalizeCurrentDeliveryFailure(ctx, matrix, SQLDeliveryAssemblyStore{DB: p.DB}, lease, plan)
		if err != nil {
			return "", err
		}
		if finalized {
			return "finalized_failed", nil
		}
		return "pending", nil
	}
	if state != "assemble" {
		return state, nil
	}
	frozen, err := p.prepare(ctx, lease, plan, root)
	if err != nil {
		return "", err
	}
	if _, err := p.register(ctx, frozen, lease, plan); err != nil {
		return "", err
	}
	if err := (SQLDeliveryAssemblyStore{DB: p.DB}).Activate(ctx, frozen, lease, plan); err != nil {
		return "", err
	}
	return "activated", nil
}

// Admission callbacks contain SQL only; object publication stays outside.
func assemblyTenantTransaction(ctx context.Context,db delivery.MetadataTransactionBeginner,l delivery.ExpansionLease,fn func(context.Context,pgx.Tx)error)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil||!l.Valid()||fn==nil{return v2input.ErrInvalid}
 return (sqlWatermarkEncodeAdmission{db:db,lease:l}).transaction(ctx,fn)
}
