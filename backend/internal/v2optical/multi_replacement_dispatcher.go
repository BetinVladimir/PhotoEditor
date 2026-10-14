package v2optical
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type MultiReplacementWaveService interface{
 AdvanceProjectMultiReplacementWave(context.Context,auth.Identity,uuid.UUID,uuid.UUID,NegativeDeclinedDecision,int)(MultiReplacementWavePass,error)
}
// Wrap the existing negative/perspective dispatcher. Each configured operation
// reloads durable proofs; only new confirmed admissions consume the shared budget.
type MultiReplacementDispatcher struct{
 Base NegativeReplacementWaveService
 Panorama,FocusStack MultiReplacementWaveService
}
func(d MultiReplacementDispatcher)AdvanceProjectReplacementWave(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,decision NegativeDeclinedDecision,limit int)(NegativeReplacementWavePass,error){
 var zero NegativeReplacementWavePass
 if err:=ctx.Err();err!=nil{return zero,err}
 if d.Base==nil||limit<1||limit>100{return zero,v2input.ErrInvalid}
 out,err:=d.Base.AdvanceProjectReplacementWave(ctx,who,tenant,project,decision,limit);if err!=nil{return out,err}
 if out.WaitingForSettlement{return out,ctx.Err()}
 for _,service:=range []MultiReplacementWaveService{d.Panorama,d.FocusStack}{
  if err:=ctx.Err();err!=nil{return out,err}
  remaining:=limit-out.Publications
  if service==nil||remaining<=0||len(out.Deferred)==0{continue}
  pass,err:=service.AdvanceProjectMultiReplacementWave(ctx,who,tenant,project,decision,remaining)
  out.MultiReceipts=append(out.MultiReceipts,pass.Receipts...)
  out.Pending=append(out.Pending,pass.Pending...);out.Unconfirmed=append(out.Unconfirmed,pass.Unconfirmed...)
  handled:=map[uuid.UUID]bool{}
  for _,receipt:=range pass.Receipts{handled[receipt.OriginalTask]=true;if !receipt.RecoveredExisting{out.Publications++}}
  for _,node:=range pass.Pending{handled[node.OriginalTask]=true}
  // Preserve original deferred inventory on failure, alongside confirmed receipts.
  if err!=nil{return out,err}
  if pass.WaitingForSettlement{out.WaitingForSettlement=true;return out,ctx.Err()}
  deferred:=make([]NegativeReplacementNode,0,len(out.Deferred))
  for _,node:=range out.Deferred{if !handled[node.OriginalTask]{deferred=append(deferred,node)}}
  out.Deferred=deferred
 }
 return out,ctx.Err()
}
var _ NegativeReplacementWaveService=MultiReplacementDispatcher{}
var _ MultiReplacementWaveService=(*MultiProjects)(nil)
