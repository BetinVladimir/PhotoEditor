package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// SaveNegativeReplacementDraft persists only a preliminary immutable run and
// workflow after current whole-wave/source/qualification locks. It does not
// reserve output space or publish runnable tasks. An ambiguous commit is returned
// unchanged; recovery must read and compare the same deterministic draft.
func(p PreparedNegativeReplacementInputs)SaveNegativeReplacementDraft(ctx context.Context,db NegativeTransactions,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft NegativeReplacementDraft,snapshot uuid.UUID,located LocatedNegativeQualification,locks v2qualification.DeploymentLocks,capacity NegativeResourceEstimate)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil||snapshot==uuid.Nil||len(draft.raw)<2||len(draft.raw)>3<<20{return v2input.ErrInvalid}
 if _,_,_,err:=p.checked(ctx);err!=nil{return err}
 // Reject unqualified or incomplete packages before obtaining a transaction.
 qualified,err:=located.Qualification();if err!=nil{return err}
 pin,err:=qualified.Pin();if err!=nil{return err}
 ready,err:=p.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return err}
 matched:=0
 for _,step:=range ready{
  if step.parents.step.intent.locator.Operation!="optical.negative"{continue}
  if _,err:=step.RestoreNegativeReplacementDraft(ctx,draft.Bytes(),snapshot,pin,locks,qualified.reportRaw,qualified.calibrationRaw,capacity);err==nil{matched++}
  if err:=ctx.Err();err!=nil{return err}
 }
 if matched!=1{return v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 checked,err:=p.LockNegativeReplacementDraft(ctx,tx,rebuilt,retained,mappings,unchanged,draft.Bytes(),snapshot,located,locks,capacity);if err!=nil{return err}
 var saved bool
 if err=tx.QueryRow(ctx,"SELECT photo.save_v29_negative_replacement_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8)",p.tenant,p.project,p.who.UserID,p.who.SessionID,p.review,p.hash,p.planJCS,string(checked.Bytes())).Scan(&saved);err!=nil{return err}
 if !saved{return v2input.ErrExpired}
 if err:=ctx.Err();err!=nil{return err}
 return tx.Commit(ctx)
}
