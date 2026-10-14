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

type SignedDeliveryAssemblyRegistrationStore interface {
	Registered(context.Context,FrozenSignedDeliveryAssemblyTask,delivery.ExpansionLease,delivery.PreparePlan)(bool,error)
	Create(context.Context,FrozenSignedDeliveryAssemblyTask,delivery.ExpansionLease,delivery.PreparePlan)error
	Renew(context.Context,delivery.ExpansionLease)error
	Register(context.Context,FrozenSignedDeliveryAssemblyTask,delivery.ExpansionLease,delivery.PreparePlan,v2input.GraphTask)error
}

// RegisterPreparedSignedAssembly commits the blocked shell before immutable
// input-document IO. A ready receipt permits replay without republishing.
func(f FrozenSignedDeliveryAssemblyTask)RegisterPreparedSignedAssembly(ctx context.Context,
	store SignedDeliveryAssemblyRegistrationStore,publisher v2input.Publisher,
	lease delivery.ExpansionLease,plan delivery.PreparePlan)(uuid.UUID,error){
	if err:=ctx.Err();err!=nil{return uuid.Nil,err}
	if store==nil||publisher==nil||!lease.Valid()||lease.Cursor!=lease.SlotCount||
		lease.ParentTask!=plan.TaskID(){return uuid.Nil,v2input.ErrInvalid}
	draft,err:=f.DraftTask();if err!=nil{return uuid.Nil,err}
	owner,err:=v2input.TaskID(lease.Workflow,"v2.delivery.assemble",0)
	if err!=nil||owner!=draft.ID{return uuid.Nil,v2input.ErrInvalid}
	ready,err:=store.Registered(ctx,f,lease,plan)
	if err!=nil{return uuid.Nil,err}
	if err:=ctx.Err();err!=nil{return uuid.Nil,err}
	if ready{return draft.ID,nil}
	if err:=store.Create(ctx,f,lease,plan);err!=nil{return uuid.Nil,err}
	if err:=ctx.Err();err!=nil{return uuid.Nil,err}
	if err:=store.Renew(ctx,lease);err!=nil{return uuid.Nil,err}
	if err:=ctx.Err();err!=nil{return uuid.Nil,err}
	graph,err:=f.PublishInputs(ctx,publisher)
	if err!=nil{return uuid.Nil,err}
	if err:=ctx.Err();err!=nil{return uuid.Nil,err}
	if err:=store.Renew(ctx,lease);err!=nil{return uuid.Nil,err}
	if err:=ctx.Err();err!=nil{return uuid.Nil,err}
	if err:=store.Register(ctx,f,lease,plan,graph);err!=nil{return uuid.Nil,err}
	if err:=ctx.Err();err!=nil{return uuid.Nil,err}
	return draft.ID,nil
}

type SQLSignedDeliveryAssemblyStore struct{ DB *pgxpool.Pool }

func(s SQLSignedDeliveryAssemblyStore)Registered(ctx context.Context,f FrozenSignedDeliveryAssemblyTask,
	l delivery.ExpansionLease,p delivery.PreparePlan)(bool,error){
	if s.DB==nil{return false,v2input.ErrInvalid}
	draft,err:=f.DraftTask();if err!=nil{return false,err}
	var ready bool
	err=assemblyTenantTransaction(ctx,s.DB,l,func(call context.Context,tx pgx.Tx)error{
		var readErr error
		ready,readErr=(delivery.ExpansionLeaseStore{DB:tx}).SignedAssemblyTransportReady(call,l,p,draft,f.task.closure.ParamsJCS)
		return readErr
	})
	return ready,err
}
func(s SQLSignedDeliveryAssemblyStore)Create(ctx context.Context,f FrozenSignedDeliveryAssemblyTask,
	l delivery.ExpansionLease,p delivery.PreparePlan)error{
	if s.DB==nil{return v2input.ErrInvalid}
	return assemblyTenantTransaction(ctx,s.DB,l,func(call context.Context,tx pgx.Tx)error{
		return f.CreateInTransaction(call,tx,l,p)
	})
}
func(s SQLSignedDeliveryAssemblyStore)Renew(ctx context.Context,l delivery.ExpansionLease)error{
	return (SQLDeliveryAssemblyStore{DB:s.DB}).Renew(ctx,l)
}
func(s SQLSignedDeliveryAssemblyStore)Register(ctx context.Context,f FrozenSignedDeliveryAssemblyTask,
	l delivery.ExpansionLease,p delivery.PreparePlan,g v2input.GraphTask)error{
	if s.DB==nil{return v2input.ErrInvalid}
	return assemblyTenantTransaction(ctx,s.DB,l,func(call context.Context,tx pgx.Tx)error{
		return f.RegisterInputsInTransaction(call,tx,l,p,g)
	})
}

// RegisterPublishedSignedAssembly installs only the blocked task and transport;
// final activation must revalidate current signer/operator/physical evidence.
func(p DeliveryAssemblyPipeline)RegisterPublishedSignedAssembly(ctx context.Context,
	lease delivery.ExpansionLease,plan delivery.PreparePlan,frozen FrozenSignedDeliveryAssemblyTask)(uuid.UUID,error){
	if p.DB==nil||p.Objects==nil||len(p.Bucket)<3||len(p.Bucket)>63{return uuid.Nil,v2input.ErrInvalid}
	draft,err:=frozen.DraftTask();if err!=nil{return uuid.Nil,err}
	transport:=deliveryAssemblyTransportQueries{db:p.DB,lease:lease,task:draft.ID}
	documents:=artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:transport,Bucket:p.Bucket},Objects:p.Objects}
	publisher:=v2input.ScopedPublisher{Documents:documents,Tenant:lease.Tenant,Run:lease.Run}
	return frozen.RegisterPreparedSignedAssembly(ctx,SQLSignedDeliveryAssemblyStore{DB:p.DB},publisher,lease,plan)
}
