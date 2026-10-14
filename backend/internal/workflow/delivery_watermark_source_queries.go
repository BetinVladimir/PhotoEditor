package workflow

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

const watermarkChildSourcesQuery=`SELECT photo.v30_watermark_child_sources($1,$2,$3,$4,$5)`

// watermarkChildSourceQueries accepts only this lease/slot/exact frozen request.
// Each Scan commits before returning bytes; physical IO cannot retain SQL locks.
// It is intentionally private and cannot route arbitrary SQL or another child.
type watermarkChildSourceQueries struct {
 db delivery.MetadataTransactionBeginner
 lease delivery.ExpansionLease
 slot int
 request string
}
type watermarkChildSourceRow struct {
 owner watermarkChildSourceQueries
 ctx context.Context
 query string
 args []any
}
func(q watermarkChildSourceQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{
 return watermarkChildSourceRow{q,ctx,query,append([]any(nil),args...)}
}
func(r watermarkChildSourceRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err}
 q:=r.owner;l:=q.lease
 if q.db==nil||!l.Valid()||q.slot<0||q.slot>l.Cursor||q.slot>=l.SlotCount||len(q.request)<2||len(q.request)>2<<20||r.query!=watermarkChildSourcesQuery||len(r.args)!=5{return v2input.ErrInvalid}
 expansion,ok:=r.args[0].(uuid.UUID);if !ok||expansion!=l.Expansion{return v2input.ErrInvalid}
 token,ok:=r.args[1].(uuid.UUID);if !ok||token!=l.Token{return v2input.ErrInvalid}
 slot,ok:=r.args[2].(int);if !ok||slot!=q.slot{return v2input.ErrInvalid}
 request,ok:=r.args[3].(string);if !ok||request!=q.request{return v2input.ErrInvalid}
 after,ok:=r.args[4].(int);if !ok||after < -1||after>1000000{return v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(r.ctx,30*time.Second);defer cancel()
 tx,err:=q.db.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(r.ctx),5*time.Second);defer stop();_=tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,l.Tenant.String());err!=nil{return err}
 if err=tx.QueryRow(ctx,r.query,r.args...).Scan(dest...);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err};if err=tx.Commit(ctx);err!=nil{return err}
 return r.ctx.Err()
}
