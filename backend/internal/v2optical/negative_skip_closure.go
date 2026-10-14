package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// CloseNegativeSkippedDependencies writes only the immutable skip closure after
// independently revalidating settlement. A successful commit is not final run
// completion; existing finalizers still own terminal state and events.
func CloseNegativeSkippedDependencies(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,reviewID uuid.UUID,hash string,plan NegativeInvalidationPlan)error{
 return closeNegativeSkippedDependencies(ctx,db,who,tenant,project,reviewID,hash,plan,false)
}
func closeNegativeSkippedDependencies(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,reviewID uuid.UUID,hash string,plan NegativeInvalidationPlan,service bool)error{
 if err:=ctx.Err();err!=nil {return err}
 if db==nil||tenant==uuid.Nil||project==uuid.Nil||reviewID==uuid.Nil||plan.SchemaVersion!=1||plan.Route!="skip"||plan.OriginTask==uuid.Nil||plan.OriginRun==uuid.Nil||(!service&&(who.UserID==uuid.Nil||who.SessionID==uuid.Nil)) {return v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(hash);err!=nil {return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(plan);if err!=nil||len(raw)>1048576 {return v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil {return err};if tx==nil {return v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return err};if role!="photo_scheduler" {return v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil {return err}
 var actor,session any=who.UserID,who.SessionID;if service {actor=nil;session=nil}
 var closed bool
 if err:=tx.QueryRow(ctx,"SELECT photo.close_v29_negative_skipped_dependencies($1,$2,$3,$4,$5,$6,$7::jsonb)",tenant,project,actor,session,reviewID,hash,string(raw)).Scan(&closed);err!=nil {return err}
 if !closed {return v2input.ErrExpired}
 return tx.Commit(ctx)
}
