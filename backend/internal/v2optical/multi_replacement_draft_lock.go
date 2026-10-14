package v2optical

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// Caller owns the transaction. No IO, persistence, quota or queue mutation.
// Full ready-wave proofs, original consumer snapshot and current located
// qualification remain locked through the caller's final publication operation.
func(p PreparedNegativeReplacementInputs) LockMultiReplacementDraft(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,raw []byte,snapshot uuid.UUID,located LocatedMultiQualification,locks v2qualification.DeploymentLocks,capacity MultiResourceEstimate)(MultiReplacementDraft,error){
 var zero MultiReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if tx==nil||snapshot==uuid.Nil||len(raw)<2||len(raw)>40<<20{return zero,v2input.ErrInvalid}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 ready,err:=p.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return zero,err}
 var selected *PreparedMappedReplacementStep
 for i:=range ready{
  if ready[i].parents.step.intent.locator.Operation!=qualified.operation{continue}
  if _,e:=ready[i].RestoreMultiReplacementDraft(ctx,raw,snapshot,pin,locks,qualified.reportRaw,qualified.calibrationRaw,capacity);e!=nil{if ctx.Err()!=nil{return zero,ctx.Err()};continue}
  if selected!=nil{return zero,v2input.ErrInvalid};selected=&ready[i]
 }
 if selected==nil{return zero,v2input.ErrInvalid}
 if _,err=p.LockCompleteMappedSources(ctx,tx,rebuilt,retained,mappings,unchanged);err!=nil{return zero,err}
 l:=selected.parents.step.intent.locator;who:=selected.parents.step.intent.who
 var valid bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v29_negative_replacement_snapshot($1,$2,$3,$4,$5,$6,$7,$8)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,selected.Node().OriginalTask,snapshot).Scan(&valid);err!=nil{return zero,err}
 if !valid{return zero,v2input.ErrExpired}
 if err=LockLocatedMultiQualification(ctx,tx,who,l.Tenant,l.Project,located);err!=nil{return zero,err}
 // Recompile only after retaining every relational authority lock.
 draft,err:=selected.RestoreMultiReplacementDraft(ctx,raw,snapshot,pin,locks,qualified.reportRaw,qualified.calibrationRaw,capacity);if err!=nil{return zero,err}
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v29_multi_replacement_inventory($1,$2,$3,$4,$5,$6,$7::jsonb,$8)",p.tenant,p.project,p.who.UserID,p.who.SessionID,p.review,p.hash,p.planJCS,string(draft.Bytes())).Scan(&valid);err!=nil{return zero,err}
 if !valid{return zero,v2input.ErrExpired}
 return draft,ctx.Err()
}
