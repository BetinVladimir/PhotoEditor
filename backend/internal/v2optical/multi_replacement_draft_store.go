package v2optical

import (
 "bytes"
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// SaveMultiReplacementDraft creates only a preliminary immutable run/workflow.
// Ambiguous commits propagate; recover using LoadMultiReplacementDraft with the
// same freshly compiled expected draft, never by silently overwriting a run.
func(p PreparedNegativeReplacementInputs) SaveMultiReplacementDraft(ctx context.Context,db NegativeTransactions,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft MultiReplacementDraft,snapshot uuid.UUID,located LocatedMultiQualification,locks v2qualification.DeploymentLocks,capacity MultiResourceEstimate)error{
 _,err:=p.multiReplacementDraftStore(ctx,db,rebuilt,retained,mappings,unchanged,draft,snapshot,located,locks,capacity,true)
 return err
}

// LoadMultiReplacementDraft reads exact preliminary state only. Missing,
// conflicting, sealed or stale state is expired; this path never repairs it.
func(p PreparedNegativeReplacementInputs) LoadMultiReplacementDraft(ctx context.Context,db NegativeTransactions,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,expected MultiReplacementDraft,snapshot uuid.UUID,located LocatedMultiQualification,locks v2qualification.DeploymentLocks,capacity MultiResourceEstimate)(MultiReplacementDraft,error){
 return p.multiReplacementDraftStore(ctx,db,rebuilt,retained,mappings,unchanged,expected,snapshot,located,locks,capacity,false)
}
func(p PreparedNegativeReplacementInputs) multiReplacementDraftStore(ctx context.Context,db NegativeTransactions,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,expected MultiReplacementDraft,snapshot uuid.UUID,located LocatedMultiQualification,locks v2qualification.DeploymentLocks,capacity MultiResourceEstimate,save bool)(MultiReplacementDraft,error){
 var zero MultiReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||snapshot==uuid.Nil||len(expected.raw)<2||len(expected.raw)>40<<20{return zero,v2input.ErrInvalid}
 if _,_,_,err:=p.checked(ctx);err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 ready,err:=p.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return zero,err}
 matched:=0
 for _,step:=range ready{
  if step.parents.step.intent.locator.Operation!=qualified.operation{continue}
  if _,e:=step.RestoreMultiReplacementDraft(ctx,expected.Bytes(),snapshot,pin,locks,qualified.reportRaw,qualified.calibrationRaw,capacity);e==nil{matched++}
  if err:=ctx.Err();err!=nil{return zero,err}
 }
 if matched!=1{return zero,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 checked,err:=p.LockMultiReplacementDraft(ctx,tx,rebuilt,retained,mappings,unchanged,expected.Bytes(),snapshot,located,locks,capacity);if err!=nil{return zero,err}
 args:=[]any{p.tenant,p.project,p.who.UserID,p.who.SessionID,p.review,p.hash,p.planJCS,string(checked.Bytes())}
 if save{
  var saved bool
  if err=tx.QueryRow(ctx,"SELECT photo.save_v29_multi_replacement_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8)",args...).Scan(&saved);err!=nil{return zero,err}
  if !saved{return zero,v2input.ErrExpired}
 }else{
  var stored *string
  if err=tx.QueryRow(ctx,"SELECT photo.read_v29_multi_replacement_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8)",args...).Scan(&stored);err!=nil{return zero,err}
  if stored==nil||!bytes.Equal([]byte(*stored),checked.Bytes()){return zero,v2input.ErrExpired}
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return checked,nil
}
