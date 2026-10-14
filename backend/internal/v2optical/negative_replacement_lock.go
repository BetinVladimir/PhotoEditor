package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// LockNegativeReplacementScope must precede per-source/intent/qualification
// locks in the same transaction. It does no IO/commit; false requires rollback.
func LockNegativeReplacementScope(ctx context.Context,tx pgx.Tx,who auth.Identity,tenant,project,reviewID uuid.UUID,hash string,plan NegativeInvalidationPlan)error{
 if err:=ctx.Err();err!=nil {return err}
 if tx==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenant==uuid.Nil||project==uuid.Nil||reviewID==uuid.Nil||plan.SchemaVersion!=1||plan.OriginTask==uuid.Nil||plan.OriginRun==uuid.Nil||(plan.Route!="base"&&plan.Route!="skip") {return v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(hash);err!=nil {return v2input.ErrInvalid}
 body,err:=canonical.Marshal(plan);if err!=nil||len(body)>1048576 {return v2input.ErrInvalid}
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return err};if role!="photo_scheduler" {return v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil {return err}
 var allowed bool
 if err:=tx.QueryRow(ctx,"SELECT photo.lock_v29_negative_replacement_scope($1,$2,$3,$4,$5,$6,$7::jsonb)",tenant,project,who.UserID,who.SessionID,reviewID,hash,string(body)).Scan(&allowed);err!=nil {return err}
 if !allowed {return v2input.ErrExpired};return ctx.Err()
}

// LockOpticalFrozenIntent repeats the original Params pin under retained task,
// backend-document and artifact locks. Caller first locks replacement scope.
func LockOpticalFrozenIntent(ctx context.Context,tx pgx.Tx,who auth.Identity,tenant,project,reviewID,task uuid.UUID,hash string,v VerifiedOpticalFrozenIntent)error{
 if err:=ctx.Err();err!=nil {return err};if tx==nil {return v2input.ErrInvalid}
 s:=v.locator
 if who.UserID==uuid.Nil||who.SessionID==uuid.Nil||v.who.UserID!=who.UserID||v.who.SessionID!=who.SessionID {return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(s);if err!=nil {return err}
 if _,err:=decodeNegativeFrozenIntent(raw,s.Params.Bucket,tenant,project,reviewID,task,hash);err!=nil {return err}
 sum:=sha256.Sum256([]byte(v.raw))
 if len(v.raw)<2||len(v.raw)>65536||int64(len(v.raw))!=s.Params.Bytes||hex.EncodeToString(sum[:])!=s.Params.SHA256 {return v2input.ErrInvalid}
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return err};if role!="photo_scheduler" {return v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil {return err}
 var allowed bool
 if err:=tx.QueryRow(ctx,"SELECT photo.lock_v29_negative_frozen_intent($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)",tenant,project,who.UserID,who.SessionID,reviewID,hash,task,string(raw),v.raw).Scan(&allowed);err!=nil {return err}
 if !allowed {return v2input.ErrExpired};return ctx.Err()
}
