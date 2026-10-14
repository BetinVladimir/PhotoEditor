package delivery

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
// Separate API allowlist: no scheduler query or role elevation is permitted.
type metadataSourceQueries struct {db MetadataTransactionBeginner;tenant uuid.UUID}
type metadataSourceRow struct {owner metadataSourceQueries;ctx context.Context;query string;args []any}
func(q metadataSourceQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{
 return metadataSourceRow{q,ctx,query,append([]any(nil),args...)}
}
func(r metadataSourceRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err}
 if r.owner.db==nil||r.owner.tenant==uuid.Nil||len(r.args)==0{return v2input.ErrInvalid}
 tenant,ok:=r.args[0].(uuid.UUID);if !ok||tenant!=r.owner.tenant{return v2input.ErrInvalid}
 switch r.query{
 case metadataSourceRetryQuery:
 case `SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
 FROM photo.v30_delivery_metadata_original($1,$2,$3,$4,$5,$6,$7)`,
 `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_metadata_source($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
 `SELECT photo.complete_v30_metadata_source($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`:
 default:return v2input.ErrInvalid
 }
 ctx,cancel:=context.WithTimeout(r.ctx,30*time.Second);defer cancel()
 tx,err:=r.owner.db.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(r.ctx),5*time.Second);defer stop();_ =tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{return err}
 if err=tx.QueryRow(ctx,r.query,r.args...).Scan(dest...);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 if err=tx.Commit(ctx);err!=nil{return err}
 return r.ctx.Err()
}
