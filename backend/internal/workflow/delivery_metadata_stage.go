package workflow

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// DeliveryMetadataStage composes materialization for an already authenticated
// metadata source and claimed barrier. It does not authorize client approvals,
// resolve originals, claim work or enable public admission.
type DeliveryMetadataStage struct{
 DB delivery.MetadataTransactionBeginner
 Objects artifactjson.Objects
 Bucket string
 Registry Registry
 Limits DeliveryDerivativeLimits
}
func(s DeliveryMetadataStage)Stage(ctx context.Context,lease delivery.MetadataBarrierLease,spec DeliveryMetadataSpec,encodedBytes int64)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63||lease.Tenant==uuid.Nil||
 lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.ChildRun!=spec.ChildRun||lease.Until.IsZero(){return v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(spec.ChildRun)
 if err!=nil{return err}
 predecessor,err:=v2input.TaskID(workflow,"export.encode",0)
 if err!=nil||predecessor!=lease.ParentTask{return v2input.ErrInvalid}
 // Compile all owned bytes before the first database/object callback can yield.
 draft,plan,err:=PrepareDeliveryMetadataTask(spec,s.Registry,encodedBytes,s.Limits)
 if err!=nil{return err}
 _,proof,pin,err:=delivery.CompileMetadataWriteProof(draft.ID,spec.Encoded,spec.EncodedJCS,spec.FrozenJCS,spec.FrozenSHA,
 spec.Binding,spec.Metadata,spec.MetadataJCS,spec.Policy,append([]string(nil),spec.ApprovedFields...))
 if err!=nil{return err}
 scope:=delivery.MetadataProofScope{Tenant:lease.Tenant,Expansion:lease.Expansion,Token:lease.Token,ChildRun:spec.ChildRun,
 Task:draft.ID,Encoded:spec.Encoded,Metadata:spec.Metadata,Binding:spec.Binding,Policy:spec.Policy,ApprovedFields:append([]string(nil),spec.ApprovedFields...)}
 taskBody,err:=canonical.Marshal(draft)
 if err!=nil{return err}
 resource,err:=ParseResourceEstimate(draft.ExecutionProfile)
 if err!=nil{return err}
 lifecycle,err:=delivery.NewMetadataTaskLifecycle(s.DB,lease.Tenant)
 if err!=nil{return err}
 if activated,err:=lifecycle.Activated(ctx,scope,taskBody);err!=nil||activated{return err}
 leases,err:=delivery.NewMetadataBarrierLeaseStore(s.DB,lease.Tenant)
 if err!=nil{return err}
 if err=leases.Renew(ctx,lease);err!=nil{return err}
 proofs,err:=delivery.NewMetadataProofStore(s.DB,lease.Tenant)
 if err!=nil{return err}
 if err=proofs.Pin(ctx,scope,proof,pin);err!=nil{return err}
 documents,err:=delivery.NewMetadataDocumentRepository(s.DB,scope,plan,s.Bucket)
 if err!=nil{return err}
 if err=delivery.PublishMetadataDocuments(ctx,documents,s.Objects);err!=nil{return err}
 if err=leases.Renew(ctx,lease);err!=nil{return err}
 if ready,err:=lifecycle.TransportReady(ctx,scope,taskBody);err!=nil{return err}else if !ready{
  if err=lifecycle.Create(ctx,scope,taskBody,resource.OutputBytes);err!=nil{return err}
  publisher,err:=delivery.NewMetadataTransportPublisher(ctx,s.DB,scope,plan,s.Bucket,s.Objects)
  if err!=nil{return err}
  graph,err:=plan.PublishInputs(ctx,spec.ChildRun,publisher)
  if err!=nil{return err}
  if err=leases.Renew(ctx,lease);err!=nil{return err}
  if err=s.register(ctx,scope,plan,draft,graph,resource.OutputBytes);err!=nil{return err}
 }
 if err=ctx.Err();err!=nil{return err}
 return lifecycle.Activate(ctx,scope,taskBody)
}
func(s DeliveryMetadataStage)register(ctx context.Context,scope delivery.MetadataProofScope,plan delivery.MetadataWritePlan,draft v2input.DraftTask,graph v2input.GraphTask,reserved int64)error{
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
 if err!=nil{return err}
 defer func(){cleanup,cancel:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer cancel();_ =tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,scope.Tenant.String());err!=nil{return err}
 if err=plan.RegisterInputsInTransaction(ctx,tx,scope,draft,graph,reserved);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 return tx.Commit(ctx)
}
