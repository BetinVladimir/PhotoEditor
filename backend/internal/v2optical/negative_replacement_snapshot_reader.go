package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func(p PreparedNegativeReplacementInputs)readOriginalSnapshot(ctx context.Context,db NegativeTransactions,original uuid.UUID)(uuid.UUID,error){
 if err:=ctx.Err();err!=nil{return uuid.Nil,err};if db==nil||original==uuid.Nil{return uuid.Nil,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return uuid.Nil,err};if tx==nil{return uuid.Nil,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return uuid.Nil,err};if role!="photo_scheduler"{return uuid.Nil,v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",p.tenant.String());err!=nil{return uuid.Nil,err}
 var snapshot *uuid.UUID
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_replacement_snapshot($1,$2,$3,$4,$5,$6,$7)",p.tenant,p.project,p.who.UserID,p.who.SessionID,p.review,p.hash,original).Scan(&snapshot);err!=nil{return uuid.Nil,err}
 if snapshot==nil||*snapshot==uuid.Nil{return uuid.Nil,v2input.ErrExpired}
 if err=tx.Commit(ctx);err!=nil{return uuid.Nil,err};return *snapshot,nil
}

// ReadOriginalReplacementSnapshot exposes the existing source-authorized
// snapshot reader for operation-specific scheduler recovery. No snapshot writes.
func(p PreparedNegativeReplacementInputs)ReadOriginalReplacementSnapshot(ctx context.Context,db NegativeTransactions,original uuid.UUID)(uuid.UUID,error){
 return p.readOriginalSnapshot(ctx,db,original)
}
