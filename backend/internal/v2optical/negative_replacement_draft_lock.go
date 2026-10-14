package v2optical

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// LockNegativeReplacementDraft binds a freshly recompiled draft to the entire
// locked ready wave, original consumer snapshot and current qualification.
// Caller must retain this transaction through persistence and roll back every
// error. This gate creates no run, task, reservation or admission receipt.
func(p PreparedNegativeReplacementInputs)LockNegativeReplacementDraft(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,raw []byte,snapshot uuid.UUID,located LocatedNegativeQualification,locks v2qualification.DeploymentLocks,capacity NegativeResourceEstimate)(NegativeReplacementDraft,error){
 var zero NegativeReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 ready,err:=p.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return zero,err}
 // Match by full canonical compilation, never by an untrusted task UUID alone.
 var selected *PreparedMappedReplacementStep
 var draft NegativeReplacementDraft
 for i:=range ready{
  if ready[i].parents.step.intent.locator.Operation!="optical.negative"{continue}
  candidate,e:=ready[i].RestoreNegativeReplacementDraft(ctx,raw,snapshot,pin,locks,qualified.reportRaw,qualified.calibrationRaw,capacity)
  if e!=nil{if ctx.Err()!=nil{return zero,ctx.Err()};continue}
  if selected!=nil{return zero,v2input.ErrInvalid};selected=&ready[i];draft=candidate
 }
 if selected==nil{return zero,v2input.ErrInvalid}
 if _,err=p.LockCompleteMappedSources(ctx,tx,rebuilt,retained,mappings,unchanged);err!=nil{return zero,err}
 l:=selected.parents.step.intent.locator;who:=selected.parents.step.intent.who
 var valid bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v29_negative_replacement_snapshot($1,$2,$3,$4,$5,$6,$7,$8)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,selected.Node().OriginalTask,snapshot).Scan(&valid);err!=nil{return zero,err}
 if !valid{return zero,v2input.ErrExpired}
 if err=LockLocatedNegativeQualification(ctx,tx,who,l.Tenant,l.Project,located);err!=nil{return zero,err}
 return draft,ctx.Err()
}
