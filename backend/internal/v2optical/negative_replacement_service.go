package v2optical

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Admission of a detector prerequisite never acknowledges its perspective replacement.
type ReplacementDetectorAdmission struct {
 OriginalTask,Run,Task uuid.UUID
 RecoveredExisting bool
}

type NegativeReplacementWavePass struct {
 DetectorAdmissions []ReplacementDetectorAdmission
 DetectorPending,DetectorUnconfirmed []NegativeReplacementNode
 WaitingForSettlement bool
 Total,Adopted,Ready int
 // New admissions only; recovered receipts do not consume publication capacity.
 Publications int
 Receipts []NegativeReplacementReceipt
 PerspectiveReceipts []ManualPerspectiveReplacementReceipt
 MultiReceipts []MultiReplacementReceipt
 // Explicitly report operations whose publication service is not yet wired.
 // Qualified ready nodes not attempted because this pass exhausted capacity.
 Pending []NegativeReplacementNode
 // A failed attempt needs a fresh durable probe; no absence or success inferred.
 Unconfirmed []NegativeReplacementNode
 Deferred []NegativeReplacementNode
}

// AdvanceReplacementWave is a bounded base-route service pass. Each invocation
// reloads durable decision/graph, original-version inputs and adopted parents.
// Returned receipts acknowledge publication only; no branch/review is completed.
// On failure, already verified receipts are returned with the error so they are
// not lost; the next pass must still probe durable state independently.
func(s *NegativeService)AdvanceReplacementWave(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,decision NegativeDeclinedDecision,limit int)(NegativeReplacementWavePass,error){
 out:=NegativeReplacementWavePass{Receipts:[]NegativeReplacementReceipt{},Deferred:[]NegativeReplacementNode{}}
 if err:=ctx.Err();err!=nil{return out,err}
 if s==nil||tenant!=s.tenant||project!=s.project||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||decision.Route!="base"||limit<1||limit>100{return out,v2input.ErrInvalid}
 c:=s.coordinator
 if c.DB==nil||c.Reader==nil||c.Writer==nil{return out,v2input.ErrInvalid}
 plan,settlement,err:=ReconcileNegativeInvalidation(ctx,c.DB,who,tenant,project,decision.Review,decision.OriginTask,decision.OriginRun,decision.DecisionHash,decision.Route,4096,65536);if err!=nil{return out,err}
 if !settlement.Settled{out.WaitingForSettlement=true;return out,nil}
 prepared,err:=LoadNegativeReplacementInputs(ctx,c.DB,c.Reader,c.Bucket,who,tenant,project,decision);if err!=nil{return out,err}
 _,current,_,err:=prepared.checked(ctx);if err!=nil{return out,err}
 before,err:=canonical.Hash(plan);if err!=nil{return out,err};after,err:=canonical.Hash(current);if err!=nil{return out,err}
 if before!=after{return out,v2input.ErrExpired}
 topology,err:=prepared.Topology();if err!=nil{return out,err};out.Total=len(topology.Nodes)
 rebuilt,retained,err:=prepared.LoadAdoptedReplacementParents(ctx,c.DB,c.Reader);if err!=nil{return out,err};out.Adopted=len(rebuilt)
 mappings,unchanged,err:=prepared.LoadMappedReadySources(ctx,c.DB,c.Reader,rebuilt,retained);if err!=nil{return out,err}
 ready,err:=prepared.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return out,err};out.Ready=len(ready)
 requests:=make([]NegativeReplacementExecutionRequest,0)
 selected:=make(map[uuid.UUID]NegativeReplacementNode)
 // Qualify every selected publication before beginning the first object write.
 for _,step:=range ready{
  if err:=ctx.Err();err!=nil{return out,err}
  if step.parents.step.Operation()!="optical.negative"{
   node:=step.Node();node.Parents=append([]NegativeReplacementParent(nil),node.Parents...);out.Deferred=append(out.Deferred,node);continue
  }
  intent,err:=step.Rebind(ctx,nil);if err!=nil{return out,err}
  params,err:=ParseNegativeParams(intent.Params());if err!=nil{return out,err}
  source,err:=step.sourceDocument(ctx,params.Base.Source);if err!=nil{return out,err}
  image,err:=VerifyNegativeWorkingImage(params,source);if err!=nil{return out,err}
  required,err:=NegativeResources(image.Canvas.Width,image.Canvas.Height,image.Alpha!=nil);if err!=nil{return out,err}
  capacity,err:=negativeRequestCapacity(required,c.Capacity);if err!=nil{return out,err}
  located,err:=LoadNegativeQualificationEvidence(ctx,SQLNegativeQualificationCatalog{DB:c.DB,Bucket:c.Bucket},c.Reader,who,tenant,project,c.Pin,c.Locks,params,image.Color.ICCSHA256);if err!=nil{return out,err}
  snapshot,err:=prepared.readOriginalSnapshot(ctx,c.DB,step.Node().OriginalTask);if err!=nil{return out,err}
  node:=step.Node();node.Parents=append([]NegativeReplacementParent(nil),node.Parents...);selected[node.OriginalTask]=node
  requests=append(requests,NegativeReplacementExecutionRequest{OriginalTask:step.Node().OriginalTask,Snapshot:snapshot,Rebuilt:rebuilt,Retained:retained,Mappings:mappings,Unchanged:unchanged,Qualification:located,Locks:c.Locks,Capacity:capacity})
 }
 for index,request:=range requests{
  if out.Publications>=limit{
   for _,pending:=range requests[index:]{out.Pending=append(out.Pending,selected[pending.OriginalTask])}
   break
  }
  receipt,err:=prepared.ExecuteNegativeReplacement(ctx,c.DB,c.Writer,c.Bucket,request)
  if err!=nil{
   out.Unconfirmed=append(out.Unconfirmed,selected[request.OriginalTask])
   for _,pending:=range requests[index+1:]{out.Pending=append(out.Pending,selected[pending.OriginalTask])}
   return out,err
  }
  out.Receipts=append(out.Receipts,receipt)
  if !receipt.RecoveredExisting{out.Publications++}
 }
 return out,ctx.Err()
}
