package v2optical

import (
 "context"
 "bytes"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
)

// NegativeDraftSnapshot binds immutable compiler output to a server-owned draft.
// RequestSHA is the idempotency conflict boundary; identity alone is insufficient.
// This is preliminary state, not publication/admission or a queued-job receipt.
type NegativeDraftSnapshot struct {
 SchemaVersion int `json:"schema_version"`
 Owner NegativeRecoveryOwner `json:"owner"`
 ParamsDocument uuid.UUID `json:"params_document_id"`
 ParamsSHA string `json:"params_sha"`
 RequestSHA string `json:"request_sha"`
 InputDigest string `json:"input_digest"`
 RecoveryJCS string `json:"recovery_jcs"`
 ReservedBytes int64 `json:"reserved_bytes"`
}

type NegativeDraft struct { raw string }

func (d NegativeDraft) Snapshot()(NegativeDraftSnapshot,error){
 var s NegativeDraftSnapshot
 if len(d.raw)<2||len(d.raw)>3<<20||json.Unmarshal([]byte(d.raw),&s)!=nil||s.SchemaVersion!=1||!s.Owner.valid(){return NegativeDraftSnapshot{},v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(s);if err!=nil||!bytes.Equal(encoded,[]byte(d.raw)){return NegativeDraftSnapshot{},v2input.ErrInvalid}
 return s,nil
}

// CompileNegativeDraft has no SQL/object side effects. Current physical source,
// qualification, quotas and identity must still be checked when saving/admitting.
func CompileNegativeDraft(ctx context.Context,who auth.Identity,tenant,project,snapshot uuid.UUID,key string,paramsRaw,sourceRaw []byte,qualified VerifiedNegativeQualification,capacity NegativeResourceEstimate)(NegativeDraft,error){
 var zero NegativeDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if tenant==uuid.Nil||project==uuid.Nil||snapshot==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||!idempotency.ValidKey(key){return zero,v2input.ErrInvalid}
 paramsRaw=append([]byte(nil),paramsRaw...);sourceRaw=append([]byte(nil),sourceRaw...)
 params,err:=ParseNegativeParams(paramsRaw);if err!=nil{return zero,err}
 plan,err:=CompileNegativeTaskPlan(ctx,params.Base,paramsRaw,sourceRaw,qualified,capacity);if err!=nil{return zero,err}
 run:=uuid.NewSHA1(tenant,[]byte("V29.Negative:"+project.String()+":"+who.UserID.String()+":"+key))
 workflow,err:=v2input.WorkflowID(run);if err!=nil{return zero,err}
 task,err:=v2input.TaskID(workflow,"optical.negative",0);if err!=nil{return zero,err}
 owner:=NegativeRecoveryOwner{TenantID:tenant,ProjectID:project,SnapshotID:snapshot,ActorID:who.UserID,AuthSessionID:who.SessionID,RunID:run,WorkflowID:workflow,TaskID:task}
 paramsRef:=v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("params")),SHA256:plan.ParamsSHA}
 recovery,err:=FreezeOwnedNegativeRecovery(ctx,owner,plan,sourceRaw,qualified,paramsRef);if err!=nil{return zero,err}
 entries,_,err:=NegativeTaskInputEntries(ctx,plan,sourceRaw,qualified,paramsRef);if err!=nil{return zero,err}
 inputDigest,err:=negativeDraftInputDigest(task,entries);if err!=nil{return zero,err}
 requestSHA,err:=negativeDraftRequestSHA(owner,plan.ParamsSHA);if err!=nil{return zero,err}
 raw,err:=canonical.Marshal(NegativeDraftSnapshot{1,owner,paramsRef.ArtifactID,plan.ParamsSHA,requestSHA,inputDigest,string(recovery),plan.ReservedBytes})
 if err!=nil||len(raw)>3<<20{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return NegativeDraft{raw:string(raw)},nil
}

func negativeDraftRequestSHA(owner NegativeRecoveryOwner,paramsSHA string)(string,error){
 return canonical.Hash(struct{
  Schema int `json:"schema_version"`
  Operation string `json:"operation"`
  Project uuid.UUID `json:"project_id"`
  Snapshot uuid.UUID `json:"snapshot_id"`
  ParamsSHA string `json:"params_sha"`
 }{1,"optical.negative",owner.ProjectID,owner.SnapshotID,paramsSHA})
}
func negativeDraftInputDigest(task uuid.UUID,entries []v2input.Entry)(string,error){
 return opticalDraftInputDigest(task,"optical.negative",entries)
}

// The operation is part of the immutable inventory identity. Replacement
// publishers must never reuse another operation's digest namespace.
func opticalDraftInputDigest(task uuid.UUID,operation string,entries []v2input.Entry)(string,error){
 if task==uuid.Nil{return "",v2input.ErrInvalid}
 switch operation{case "optical.negative","optical.perspective","optical.panorama","optical.focus_stack":default:return "",v2input.ErrInvalid}
 return canonical.Hash(struct{
  Task uuid.UUID `json:"task"`
  Operation string `json:"operation"`
  Version string `json:"version"`
  Entries []v2input.Entry `json:"entries"`
 }{task,operation,"1",entries})
}

// RestorePlan reconstructs the complete saved contract against current installed
// qualification. It does not grant live object/tenant permissions or publish work.
func(d NegativeDraft)RestorePlan(ctx context.Context,qualified VerifiedNegativeQualification)(NegativeTaskPlan,v2input.Ref,[]byte,error){
 fail:=func()(NegativeTaskPlan,v2input.Ref,[]byte,error){return NegativeTaskPlan{},v2input.Ref{},nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return NegativeTaskPlan{},v2input.Ref{},nil,err}
 s,err:=d.Snapshot();if err!=nil{return fail()}
 plan,params,source,err:=RestoreOwnedNegativeRecovery(ctx,[]byte(s.RecoveryJCS),s.Owner,qualified);if err!=nil{return NegativeTaskPlan{},v2input.Ref{},nil,err}
 if params.ArtifactID!=s.ParamsDocument||params.SHA256!=s.ParamsSHA||plan.ReservedBytes!=s.ReservedBytes||params.ArtifactID!=uuid.NewSHA1(s.Owner.TaskID,[]byte("params")){return fail()}
 workflow,err:=v2input.WorkflowID(s.Owner.RunID);if err!=nil||workflow!=s.Owner.WorkflowID{return fail()}
 task,err:=v2input.TaskID(workflow,"optical.negative",0);if err!=nil||task!=s.Owner.TaskID{return fail()}
 entries,_,err:=NegativeTaskInputEntries(ctx,plan,source,qualified,params);if err!=nil{return NegativeTaskPlan{},v2input.Ref{},nil,err}
 inputDigest,err:=negativeDraftInputDigest(task,entries);if err!=nil||inputDigest!=s.InputDigest{return fail()}
 requestSHA,err:=negativeDraftRequestSHA(s.Owner,plan.ParamsSHA);if err!=nil||requestSHA!=s.RequestSHA{return fail()}
 return plan,params,source,ctx.Err()
}

// RestoreNegativeDraft is the bounded persistence decoder. expected is resolved
// by the trusted owner lookup, never accepted from the document itself.
func RestoreNegativeDraft(ctx context.Context,raw []byte,expected NegativeRecoveryOwner,qualified VerifiedNegativeQualification)(NegativeDraft,error){
 var zero NegativeDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if !expected.valid()||len(raw)<2||len(raw)>3<<20{return zero,v2input.ErrInvalid}
 draft:=NegativeDraft{raw:string(raw)}
 snapshot,err:=draft.Snapshot();if err!=nil||snapshot.Owner!=expected{return zero,v2input.ErrInvalid}
 if _,_,_,err:=draft.RestorePlan(ctx,qualified);err!=nil{return zero,err}
 return draft,ctx.Err()
}
