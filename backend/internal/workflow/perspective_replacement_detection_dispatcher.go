package workflow

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type ReplacementDetectionWaveAdvancer interface {
 Advance(context.Context,auth.Identity,uuid.UUID,uuid.UUID,v2optical.NegativeDeclinedDecision,int)(ReplacementDetectionWavePass,error)
}

// Detector admission is a prerequisite, not a replacement publication. Only
// confirmed automatic perspective receipts remove nodes from Deferred.
type ReplacementDetectionWaveDispatcher struct {
 Base v2optical.NegativeReplacementWaveService
 Detection ReplacementDetectionWaveAdvancer
}
func(d ReplacementDetectionWaveDispatcher) AdvanceProjectReplacementWave(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,decision v2optical.NegativeDeclinedDecision,limit int)(v2optical.NegativeReplacementWavePass,error){
 var out v2optical.NegativeReplacementWavePass
 if err:=ctx.Err();err!=nil{return out,err}
 if d.Base==nil||d.Detection==nil||limit<1||limit>100{return out,v2input.ErrInvalid}
 out,err:=d.Base.AdvanceProjectReplacementWave(ctx,who,tenant,project,decision,limit)
 if err!=nil||out.WaitingForSettlement||len(out.Deferred)==0{return out,err}
 remaining:=limit-out.Publications
 if remaining<=0{return out,ctx.Err()}
 pass,err:=d.Detection.Advance(ctx,who,tenant,project,decision,remaining)
 out.Publications+=pass.Publications
 out.PerspectiveReceipts=append(out.PerspectiveReceipts,pass.AutomaticReceipts...)
 if len(pass.AutomaticReceipts)>0{
  published:=map[uuid.UUID]bool{}
  for _,receipt:=range pass.AutomaticReceipts{published[receipt.OriginalTask]=true}
  remainingDeferred:=make([]v2optical.NegativeReplacementNode,0,len(out.Deferred))
  for _,node:=range out.Deferred{if !published[node.OriginalTask]{remainingDeferred=append(remainingDeferred,node)}}
  out.Deferred=remainingDeferred
 }
 out.WaitingForSettlement=pass.WaitingForSettlement
 // Keep prerequisite state separate from replacement pending/publication state.
 out.DetectorPending=append(out.DetectorPending,pass.Pending...)
 out.DetectorUnconfirmed=append(out.DetectorUnconfirmed,pass.Unconfirmed...)
 for _,admission:=range pass.Admissions{out.DetectorAdmissions=append(out.DetectorAdmissions,v2optical.ReplacementDetectorAdmission{OriginalTask:admission.OriginalTask,Run:admission.Run,Task:admission.Task,RecoveredExisting:admission.RecoveredExisting})}
 if err!=nil{return out,err}
 return out,ctx.Err()
}
var _ v2optical.NegativeReplacementWaveService=ReplacementDetectionWaveDispatcher{}
