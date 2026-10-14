package workflow

import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
type MetadataAdoptionQueue struct{DB delivery.MetadataSchedulerDB}
func(q MetadataAdoptionQueue)SettleInvalid(ctx context.Context,c MetadataAdoptionCandidate,code string)(string,error){
 if err:=ctx.Err();err!=nil{return "",err};if q.DB==nil||!c.valid()||!metadataTerminalCode(code){return "",v2input.ErrInvalid}
 tx,err:=q.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return "",err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer stop();_ =tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,c.Tenant.String());err!=nil{return "",err}
 var state string
 err=tx.QueryRow(ctx,`SELECT photo.settle_v30_metadata_invalid($1,$2,$3,$4,$5,$6)`,
  c.Tenant,c.Task,c.Attempt,c.Fence,c.Result,code).Scan(&state)
 if err!=nil{return "",err}
 if state!="stale"&&state!="deferred"&&state!="rejected"{return "",v2input.ErrInvalid}
 if err=ctx.Err();err!=nil{return "",err};if err=tx.Commit(ctx);err!=nil{return "",err}
 return state,ctx.Err()
}
func(q MetadataAdoptionQueue)Next(ctx context.Context,after uuid.UUID)(*MetadataAdoptionCandidate,error){
 if err:=ctx.Err();err!=nil{return nil,err};if q.DB==nil{return nil,v2input.ErrInvalid}
 var c MetadataAdoptionCandidate
 err:=q.DB.QueryRow(ctx,`SELECT tenant_id,task_id,attempt,fence,result_id FROM photo.next_v30_metadata_adoption($1)`,after).Scan(&c.Tenant,&c.Task,&c.Attempt,&c.Fence,&c.Result)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err};if !c.valid(){return nil,v2input.ErrInvalid};return &c,nil
}
func(q MetadataAdoptionQueue)Committed(ctx context.Context,c MetadataAdoptionCandidate)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err};if q.DB==nil||!c.valid(){return false,v2input.ErrInvalid}
 tx,err:=q.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return false,err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer stop();_ =tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,c.Tenant.String());err!=nil{return false,err}
 var committed bool
 err=tx.QueryRow(ctx,`SELECT photo.v30_metadata_adoption_committed($1,$2,$3,$4,$5)`,c.Tenant,c.Task,c.Attempt,c.Fence,c.Result).Scan(&committed)
 if err!=nil{return false,err};if err=ctx.Err();err!=nil{return false,err};if err=tx.Commit(ctx);err!=nil{return false,err}
 return committed,ctx.Err()
}
