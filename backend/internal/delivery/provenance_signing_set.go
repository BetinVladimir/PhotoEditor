package delivery

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type SigningSetObservation struct {
 State string
 Children []SigningChildStatus
}

// ObserveDeliverySigningSet reads every ready slot under the same parent lease.
// `adopted` means all SQL receipts exist, not that their physical bytes passed
// V31 verification. The caller renews the lease between bounded pages.
func ObserveDeliverySigningSet(ctx context.Context,db ProofTransaction,lease ExpansionLease,
 actor,policy uuid.UUID,slots []Slot,renew func(context.Context)error)(SigningSetObservation,error){
 if err:=ctx.Err();err!=nil{return SigningSetObservation{},err}
 if db==nil||!lease.valid()||lease.Cursor!=lease.SlotCount||len(slots)!=lease.SlotCount||
  actor==uuid.Nil||renew==nil{return SigningSetObservation{},v2input.ErrInvalid}
 dependencies,err:=CompileSigningDependencies(lease.Run,policy,slots)
 if err!=nil{return SigningSetObservation{},err}
 result:=SigningSetObservation{State:"adopted",Children:make([]SigningChildStatus,0,len(dependencies))}
 for index,dependency:=range dependencies{
  if err:=ctx.Err();err!=nil{return SigningSetObservation{},err}
  if index%50==0{if err:=renew(ctx);err!=nil{return SigningSetObservation{},err}}
  status,err:=ReadDeliverySignerStatus(ctx,db,lease,actor,dependency)
  if err!=nil{return SigningSetObservation{},err}
  result.Children=append(result.Children,status)
  switch status.State{
  case "failed","cancelled":result.State="failed"
  case "not_started","pending","succeeded_unverified":
   if result.State!="failed"{result.State="pending"}
  case "adopted":
  default:return SigningSetObservation{},v2input.ErrInvalid
  }
 }
 return result,nil
}
