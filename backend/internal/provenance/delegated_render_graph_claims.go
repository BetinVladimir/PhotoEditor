package provenance

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

// Concrete implementations must read all render/review/qualification evidence
// under the delegated parent lease, with physical immutable-byte checks.
type DelegatedRenderReviewedReader interface{
 ReadDelegatedRenderReviewed(context.Context,ReadySigningExport,
  CanonicalSigningTask)(signingRenderReviewedCausalReceipt,error)
}
type DelegatedRenderDependencyReader interface{
 ResolveDelegatedRenderDependencies(context.Context,ReadySigningExport,
  CanonicalSigningTask,SigningRenderCausalInventory)([]SigningRenderDependencyClaim,error)
}

type DelegatedRenderSigningTaskResolver struct{
 Reviewed DelegatedRenderReviewedReader
 Dependencies DelegatedRenderDependencyReader
}

func(s DelegatedRenderSigningTaskResolver)ResolveDelegatedTask(ctx context.Context,
 ready ReadySigningExport,task CanonicalSigningTask)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Reviewed==nil||s.Dependencies==nil||ready.Tenant==uuid.Nil||
  ready.Project==uuid.Nil||ready.ExportID==uuid.Nil{
  return zero,ErrInvalid
 }
 ctx=newSigningRenderReadContext(ctx)
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 read:=func()(ResolvedSigningTask,string,error){
  isolated,err:=ownSigningProducerTask(owned)
  if err!=nil{return zero,"",err}
  receipt,err:=s.Reviewed.ReadDelegatedRenderReviewed(ctx,ready,isolated)
  if err!=nil{return zero,"",err}
  raw,err:=canonical.Marshal(receipt.causal.inventory)
  if err!=nil{return zero,"",err}
  var inventory SigningRenderCausalInventory
  if json.Unmarshal(raw,&inventory)!=nil{return zero,"",ErrInvalid}
  inventory.taskDigest=receipt.causal.inventory.taskDigest
  isolated,err=ownSigningProducerTask(owned)
  if err!=nil{return zero,"",err}
  claims,err:=s.Dependencies.ResolveDelegatedRenderDependencies(
   ctx,ready,isolated,inventory)
  if err!=nil{return zero,"",err}
  result,err:=compileSigningRenderGraphTask(owned,receipt,claims)
  return result,receipt.causal.stamp,err
 }
 result,stamp,err:=read()
 if err!=nil{return zero,err}
 current,currentStamp,err:=read()
 if err!=nil{return zero,err}
 before,err:=canonical.Hash(result)
 if err!=nil{return zero,err}
 after,err:=canonical.Hash(current)
 if err!=nil||stamp!=currentStamp||before!=after{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return current,nil
}

var _ DelegatedTaskClaims = DelegatedRenderSigningTaskResolver{}
