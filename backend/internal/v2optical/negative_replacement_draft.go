package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type negativeReplacementDraftSnapshot struct{
 SchemaVersion int `json:"schema_version"`
 Identity NegativeReplacementPublicationIdentity `json:"identity"`
 IdentitySHA string `json:"identity_sha"`
 Owner NegativeRecoveryOwner `json:"owner"`
 MarkerJCS string `json:"marker_jcs"`
 RecoveryJCS string `json:"recovery_jcs"`
 InputDigest string `json:"input_digest"`
 ReservedBytes int64 `json:"reserved_bytes"`
}
// NegativeReplacementDraft owns complete freshly compiled negative recovery
// bytes. It is preliminary data, never a saved run or admission acknowledgement.
type NegativeReplacementDraft struct{raw string}
func(d NegativeReplacementDraft)Bytes()[]byte{return []byte(d.raw)}

// RestoreNegativeReplacementDraft accepts a saved packet only when it equals a
// fresh compilation against the caller's current prepared physical inputs and
// qualification. The expected snapshot comes from authoritative consumer scope,
// never from the packet. This does not establish SQL persistence or admission.
func(p PreparedMappedReplacementStep)RestoreNegativeReplacementDraft(ctx context.Context,raw []byte,snapshot uuid.UUID,pin NegativeQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,capacity NegativeResourceEstimate)(NegativeReplacementDraft,error){
 var zero NegativeReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if snapshot==uuid.Nil||len(raw)<2||len(raw)>3<<20{return zero,v2input.ErrInvalid}
 var saved negativeReplacementDraftSnapshot
 if json.Unmarshal(raw,&saved)!=nil||saved.SchemaVersion!=2||!saved.Owner.valid()||saved.Owner.SnapshotID!=snapshot{return zero,v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(saved)
 if err!=nil||!bytes.Equal(raw,encoded){return zero,v2input.ErrInvalid}
 fresh,err:=p.CompileNegativeReplacementDraft(ctx,snapshot,pin,locks,report,calibration,capacity)
 if err!=nil{return zero,err}
 if !bytes.Equal(raw,fresh.Bytes()){return zero,v2input.ErrInvalid}
 return fresh,ctx.Err()
}

// CompileNegativeReplacementDraft reuses existing negative content qualification,
// task input enumeration and recovery freezing with dedicated replacement IDs.
// SQL must verify snapshot is the original consumer context, lock current proofs
// and qualification, and reserve resources before later publication/sealing.
func(p PreparedMappedReplacementStep)CompileNegativeReplacementDraft(ctx context.Context,snapshot uuid.UUID,pin NegativeQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,capacity NegativeResourceEstimate)(NegativeReplacementDraft,error){
 var zero NegativeReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err};if snapshot==uuid.Nil{return zero,v2input.ErrInvalid}
 identity,identitySHA,err:=p.PublicationIdentity(ctx,nil);if err!=nil{return zero,err};if identity.Operation!="optical.negative"{return zero,v2input.ErrInvalid}
 plan,qualified,err:=p.CompileNegative(ctx,pin,locks,report,calibration,capacity);if err!=nil{return zero,err}
 if plan.Operation!=identity.Operation||plan.ParamsSHA!=identity.ParamsSHA{return zero,v2input.ErrInvalid}
 params,err:=ParseNegativeParams(plan.Params);if err!=nil{return zero,err}
 source,err:=p.sourceDocument(ctx,params.Base.Source);if err!=nil{return zero,err}
 l:=p.parents.step.intent.locator;who:=p.parents.step.intent.who
 owner:=NegativeRecoveryOwner{TenantID:l.Tenant,ProjectID:l.Project,SnapshotID:snapshot,ActorID:who.UserID,AuthSessionID:who.SessionID,RunID:identity.Run,WorkflowID:identity.Workflow,TaskID:identity.Task}
 paramsRef:=v2input.Ref{ArtifactID:identity.ParamsDocument,SHA256:identity.ParamsSHA}
 recovery,err:=FreezeOwnedNegativeRecovery(ctx,owner,plan,source,qualified,paramsRef);if err!=nil{return zero,err}
 entries,_,err:=NegativeTaskInputEntries(ctx,plan,source,qualified,paramsRef);if err!=nil{return zero,err}
 inputDigest,err:=negativeDraftInputDigest(identity.Task,entries);if err!=nil{return zero,err}
 marker,markerSHA,err:=p.RunMarker(ctx);if err!=nil{return zero,err};if markerSHA!=identity.MarkerSHA{return zero,v2input.ErrInvalid}
 raw,err:=canonical.Marshal(negativeReplacementDraftSnapshot{SchemaVersion:2,Identity:identity,IdentitySHA:identitySHA,Owner:owner,MarkerJCS:string(marker),RecoveryJCS:string(recovery),InputDigest:inputDigest,ReservedBytes:plan.ReservedBytes})
 if err!=nil{return zero,err};if len(raw)>3<<20{return zero,v2input.ErrInvalid}
 return NegativeReplacementDraft{raw:string(raw)},ctx.Err()
}
