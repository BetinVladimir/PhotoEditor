package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// ActivatePublishedSignedAssembly performs physical V31 verification outside
// the short, fenced activation transaction. A committed activation is read
// back before retrying, so an uncertain commit does not repeat object IO.
func(p DeliveryAssemblyPipeline)ActivatePublishedSignedAssembly(ctx context.Context,
 lease delivery.ExpansionLease,plan delivery.PreparePlan,
 frozen FrozenSignedDeliveryAssemblyTask,publication delivery.SignedAssemblyPublication)error{
 if err:=ctx.Err();err!=nil{return err}
 if p.DB==nil||p.SignedObjects==nil||!lease.Valid()||lease.Cursor!=lease.SlotCount||
  lease.ParentTask!=plan.TaskID()||len(p.Bucket)<3||len(p.Bucket)>63{
  return v2input.ErrInvalid
 }
 draft,err:=frozen.DraftTask();if err!=nil{return err}
 owner,err:=v2input.TaskID(lease.Workflow,"v2.delivery.assemble",0)
 if err!=nil||draft.ID!=owner||publication.ClosureRef.ArtifactID==uuid.Nil||
  len(publication.Documents.ClosureJCS)<2||len(publication.Documents.ClosureJCS)>8<<20{
  return v2input.ErrInvalid
 }
 hash:=sha256.Sum256(publication.Documents.ClosureJCS)
 if hex.EncodeToString(hash[:])!=publication.ClosureRef.SHA256{return v2input.ErrInvalid}
 var closure delivery.SignedAssemblyClosure
 if json.Unmarshal(publication.Documents.ClosureJCS,&closure)!=nil||closure.SchemaVersion!=1||
  closure.TenantID!=lease.Tenant||closure.ParentID!=lease.Run||closure.ActorID==uuid.Nil||
  closure.PolicyID==uuid.Nil||len(closure.Entries)<1||len(closure.Entries)>lease.SlotCount{
  return v2input.ErrInvalid
 }
 activated:=func()(bool,error){
  var ready bool
  err:=assemblyTenantTransaction(ctx,p.DB,lease,func(call context.Context,tx pgx.Tx)error{
   var readErr error
   ready,readErr=(delivery.ExpansionLeaseStore{DB:tx}).AssemblyActivated(call,lease,plan,draft,
    frozen.task.closure.ParamsJCS)
   return readErr
  })
  return ready,err
 }
 acknowledged,err:=activated();if err!=nil{return err};if acknowledged{return nil}
 read:=func()([]delivery.SignedActivationObject,error){
  return delivery.ReadSignedActivationObjects(ctx,p.DB,lease,draft.ID,draft.ParamsArtifactID,
   draft.ParamsSHA,frozen.task.closure.ParamsJCS)
 }
 before,err:=read();if err!=nil{return err}
 renew:=func(call context.Context)error{return (SQLDeliveryAssemblyStore{DB:p.DB}).Renew(call,lease)}
 if err=delivery.VerifySignedActivationInventory(ctx,lease.Tenant,p.Bucket,closure,
  before,p.SignedObjects,renew);err!=nil{return err}
 after,err:=read();if err!=nil{return err}
 if !delivery.SignedActivationInventoryEqual(before,after){return v2input.ErrInvalid}
 return assemblyTenantTransaction(ctx,p.DB,lease,func(call context.Context,tx pgx.Tx)error{
  store:=delivery.ExpansionLeaseStore{DB:tx}
  ready,err:=store.AssemblyActivated(call,lease,plan,draft,frozen.task.closure.ParamsJCS)
  if err!=nil||ready{return err}
  return store.ActivateSignedAssembly(call,lease,plan,draft,frozen.task.closure.ParamsJCS)
 })
}
