package v2optical

import (
 "context"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// SaveNegativeDraft repeats scoped live source/qualification locks in the same
// scheduler transaction as the immutable save. No task or output quota is made
// runnable here. Physical source verification is required by the future producer
// coordinator before publication/admission, even after this preliminary save.
func SaveNegativeDraft(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant uuid.UUID,draft NegativeDraft,located LocatedNegativeQualification)error{
 if err:=ctx.Err();err!=nil{return err}
 snapshot,err:=draft.Snapshot();if err!=nil{return err}
 if db==nil||tenant==uuid.Nil||snapshot.Owner.TenantID!=tenant||snapshot.Owner.ActorID!=who.UserID||snapshot.Owner.AuthSessionID!=who.SessionID||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return v2input.ErrInvalid}
 qualified,err:=located.Qualification();if err!=nil{return err}
 plan,params,source,err:=draft.RestorePlan(ctx,qualified);if err!=nil{return err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if _,_,err:=LockNegativeAdmission(ctx,tx,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,plan,source,located,params);err!=nil{return err}
 var saved bool
 if err:=tx.QueryRow(ctx,"SELECT photo.save_v29_negative_draft($1,$2,$3,$4)",tenant,who.UserID,who.SessionID,draft.raw).Scan(&saved);err!=nil{return err}
 if !saved{return v2input.ErrExpired}
 if err:=ctx.Err();err!=nil{return err}
 return tx.Commit(ctx)
}
