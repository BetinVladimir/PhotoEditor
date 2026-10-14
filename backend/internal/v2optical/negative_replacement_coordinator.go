package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type NegativeReplacementExecutionRequest struct {
 OriginalTask uuid.UUID
 Snapshot uuid.UUID
 Rebuilt,Retained []VerifiedAdoptedParentArrays
 Mappings []VerifiedNegativeAffectedSources
 Unchanged []VerifiedNegativeUnchangedArrays
 Qualification LocatedNegativeQualification
 Locks v2qualification.DeploymentLocks
 Capacity NegativeResourceEstimate
}

type negativeReplacementPublicationSteps struct {
 probe func(context.Context)(string,error)
 params,transport,admit func(context.Context)error
 receipt func(context.Context)(NegativeReplacementReceipt,error)
}

// No error, including an ambiguous commit, is interpreted as absence or retried.
// Only a successful explicit probe permits preliminary publication to resume.
func executeNegativeReplacementPublication(ctx context.Context,steps negativeReplacementPublicationSteps)(NegativeReplacementReceipt,error){
 var zero NegativeReplacementReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if steps.probe==nil||steps.params==nil||steps.transport==nil||steps.admit==nil||steps.receipt==nil{return zero,v2input.ErrInvalid}
 state,err:=steps.probe(ctx);if err!=nil{return zero,err}
 switch state{
 case "admitted":
 case "absent","draft":
  for _,phase:=range []func(context.Context)error{steps.params,steps.transport,steps.admit}{
   if err:=ctx.Err();err!=nil{return zero,err}
   if err:=phase(ctx);err!=nil{return zero,err}
  }
 default:return zero,v2input.ErrExpired
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 receipt,err:=steps.receipt(ctx);if err!=nil{return zero,err};return receipt,nil
}

func(r negativeReplacementParamsRepository)probePublication(ctx context.Context)(string,error){
 if err:=ctx.Err();err!=nil{return "",err};if r.db==nil{return "",v2input.ErrInvalid}
 saved,_,err:=r.document(ctx);if err!=nil{return "",err}
 scope:=artifactjson.Scope{Tenant:r.prepared.tenant,Run:saved.Owner.RunID,Owner:saved.Owner.TaskID,Purpose:"task_params"}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=r.db.Begin(ctx);if err!=nil{return "",err};if tx==nil{return "",v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=r.authorize(ctx,tx,scope);err!=nil{return "",err}
 if err=r.lock(ctx,tx);err!=nil{return "",err}
 var state *string
 p:=r.prepared
 if err=tx.QueryRow(ctx,"SELECT photo.probe_v29_negative_replacement_publication($1,$2,$3,$4,$5)",p.tenant,p.project,p.who.UserID,p.who.SessionID,r.draft.raw).Scan(&state);err!=nil{return "",err}
 if state==nil||(*state!="absent"&&*state!="draft"&&*state!="admitted"){return "",v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return "",err}
 if err=tx.Commit(ctx);err!=nil{return "",err}
 return *state,nil
}

// ExecuteNegativeReplacement publishes one ready optical.negative replacement
// and returns only its independently read historical queued receipt. It does
// not await adoption, complete the review or advance other optical operations.
// Reinvoke with freshly loaded proofs after failure, preserving original IDs.
func(p PreparedNegativeReplacementInputs)ExecuteNegativeReplacement(ctx context.Context,db NegativeTransactions,objects artifactjson.Objects,bucket string,request NegativeReplacementExecutionRequest)(NegativeReplacementReceipt,error){
 var zero NegativeReplacementReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||objects==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63||request.OriginalTask==uuid.Nil||request.Snapshot==uuid.Nil{return zero,v2input.ErrInvalid}
 request.Rebuilt=append([]VerifiedAdoptedParentArrays(nil),request.Rebuilt...);request.Retained=append([]VerifiedAdoptedParentArrays(nil),request.Retained...)
 request.Mappings=append([]VerifiedNegativeAffectedSources(nil),request.Mappings...);request.Unchanged=append([]VerifiedNegativeUnchangedArrays(nil),request.Unchanged...)
 qualified,err:=request.Qualification.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 ready,err:=p.PrepareCompleteMappedSources(ctx,request.Rebuilt,request.Retained,request.Mappings,request.Unchanged);if err!=nil{return zero,err}
 var draft NegativeReplacementDraft
 matches:=0
 for _,step:=range ready{if step.Node().OriginalTask==request.OriginalTask{
  matches++;draft,err=step.CompileNegativeReplacementDraft(ctx,request.Snapshot,pin,request.Locks,qualified.reportRaw,qualified.calibrationRaw,request.Capacity);if err!=nil{return zero,err}
 }}
 if matches!=1{return zero,v2input.ErrInvalid}
 r:=negativeReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:request.Snapshot,located:request.Qualification,locks:request.Locks,capacity:request.Capacity,rebuilt:request.Rebuilt,retained:request.Retained,mappings:request.Mappings,unchanged:request.Unchanged}
 saved,_,err:=r.document(ctx);if err!=nil{return zero,err}
 published:=v2input.Ref{ArtifactID:saved.Identity.ParamsDocument,SHA256:saved.Identity.ParamsSHA}
 recovered:=false
 receipt,err:=executeNegativeReplacementPublication(ctx,negativeReplacementPublicationSteps{
  probe:func(ctx context.Context)(string,error){state,err:=r.probePublication(ctx);if err==nil&&state=="admitted"{recovered=true};return state,err},
  params:func(ctx context.Context)error{
   ref,err:=p.PublishNegativeReplacementParams(ctx,db,objects,bucket,request.Rebuilt,request.Retained,request.Mappings,request.Unchanged,draft,request.Snapshot,request.Qualification,request.Locks,request.Capacity)
   if err!=nil{return err};if ref.ID!=published.ArtifactID||ref.SHA256!=published.SHA256{return v2input.ErrInvalid};return nil
  },
  transport:func(ctx context.Context)error{_,err:=p.PublishNegativeReplacementTaskTransport(ctx,db,objects,bucket,request.Rebuilt,request.Retained,request.Mappings,request.Unchanged,draft,request.Snapshot,request.Qualification,request.Locks,request.Capacity,published);return err},
  admit:func(ctx context.Context)error{_,err:=p.AdmitNegativeReplacementGraph(ctx,db,bucket,request.Rebuilt,request.Retained,request.Mappings,request.Unchanged,draft,request.Snapshot,request.Qualification,request.Locks,request.Capacity,published);return err},
  receipt:func(ctx context.Context)(NegativeReplacementReceipt,error){return p.ReadNegativeReplacementQueuedReceipt(ctx,db,bucket,request.Rebuilt,request.Retained,request.Mappings,request.Unchanged,draft,request.Snapshot,request.Qualification,request.Locks,request.Capacity,published)},
 })
 if err!=nil{return zero,err}
 receipt.RecoveredExisting=recovered
 return receipt,nil
}
