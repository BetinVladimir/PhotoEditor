package provenance

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

type DelegatedNegativeCausalReader interface{
 ReadDelegatedNegativeCausal(context.Context,ReadySigningExport,
  CanonicalSigningTask)(signingNegativeCausalReceipt,error)
}

type DelegatedNegativeSigningTaskResolver struct{
 Causal DelegatedNegativeCausalReader
}

func(s DelegatedNegativeSigningTaskResolver)ResolveDelegatedTask(ctx context.Context,
 ready ReadySigningExport,task CanonicalSigningTask)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Causal==nil||ready.Tenant==uuid.Nil||ready.Project==uuid.Nil||
  ready.ExportID==uuid.Nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 if owned.Operation!="optical.negative"||owned.Version!="1"{
  return zero,ErrInvalid
 }
 read:=func()(ResolvedSigningTask,string,error){
  if err:=ctx.Err();err!=nil{return zero,"",err}
  isolated,err:=ownSigningProducerTask(owned)
  if err!=nil{return zero,"",err}
  receipt,err:=s.Causal.ReadDelegatedNegativeCausal(ctx,ready,isolated)
  if err!=nil{return zero,"",err}
  result,err:=compileSigningNegativeGraphTask(owned,receipt)
  return result,receipt.stamp,err
 }
 first,stamp,err:=read()
 if err!=nil{return zero,err}
 current,currentStamp,err:=read()
 if err!=nil{return zero,err}
 before,err:=canonical.Hash(first)
 if err!=nil{return zero,err}
 after,err:=canonical.Hash(current)
 if err!=nil||stamp!=currentStamp||before!=after{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return current,nil
}

var _ DelegatedTaskClaims = DelegatedNegativeSigningTaskResolver{}
