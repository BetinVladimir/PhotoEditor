package v2optical

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

type ManualPerspectiveReplacementWaveService interface {
 AdvanceProjectManualReplacementWave(context.Context,auth.Identity,uuid.UUID,uuid.UUID,NegativeDeclinedDecision,int)(ManualPerspectiveReplacementWavePass,error)
}

// OpticalReplacementDispatcher retains operation-specific qualification and
// shares a bounded publication budget. Every operation pass reloads proofs;
// no prepared authority is carried across transactions or operation services.
type OpticalReplacementDispatcher struct {
 Negative NegativeReplacementWaveService
 Perspective ManualPerspectiveReplacementWaveService
}

func(d OpticalReplacementDispatcher)AdvanceProjectReplacementWave(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,decision NegativeDeclinedDecision,limit int)(NegativeReplacementWavePass,error){
 var zero NegativeReplacementWavePass
 if err:=ctx.Err();err!=nil{return zero,err}
 if d.Negative==nil||limit<1||limit>100{return zero,v2input.ErrInvalid}
 out,err:=d.Negative.AdvanceProjectReplacementWave(ctx,who,tenant,project,decision,limit)
 if err!=nil{return out,err}
 if out.WaitingForSettlement||d.Perspective==nil{return out,ctx.Err()}
 pending:=len(out.Deferred)>0
 // Count conservative new admissions from local coordinator observations.
 // Historical acknowledgements must not starve another ready operation.
 used:=0
 for _,receipt:=range out.Receipts{if !receipt.RecoveredExisting{used++}}
 out.Publications=used
 remaining:=limit-used
 if !pending||remaining<=0{return out,ctx.Err()}
 pass,err:=d.Perspective.AdvanceProjectManualReplacementWave(ctx,who,tenant,project,decision,remaining)
 out.PerspectiveReceipts=append(out.PerspectiveReceipts,pass.Receipts...)
 out.Pending=append(out.Pending,pass.Pending...)
 out.Unconfirmed=append(out.Unconfirmed,pass.Unconfirmed...)
 for _,receipt:=range pass.Receipts{if !receipt.RecoveredExisting{out.Publications++}}
 // Failure preserves the original deferred inventory and confirmed receipts.
 if err!=nil{return out,err}
 if pass.WaitingForSettlement{out.WaitingForSettlement=true;return out,ctx.Err()}
 published:=make(map[uuid.UUID]bool,len(out.Receipts))
 for _,receipt:=range out.Receipts{published[receipt.OriginalTask]=true}
 // Capacity-pending nodes are supported work, not an unqualified operation.
 for _,node:=range out.Pending{published[node.OriginalTask]=true}
 deferred:=make([]NegativeReplacementNode,0,len(pass.Deferred))
 for _,node:=range pass.Deferred{if !published[node.OriginalTask]{deferred=append(deferred,node)}}
 out.Deferred=deferred
 return out,ctx.Err()
}

var _ NegativeReplacementWaveService=OpticalReplacementDispatcher{}
