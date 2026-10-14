package workflow

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// Closed observation adapter commits producer/recovery reads before object IO.
type watermarkObservationQueries struct{
 db delivery.MetadataTransactionBeginner
 lease delivery.ExpansionLease
 slot int
 operation string
 paramsSHA string
}
type watermarkObservationRow struct{owner watermarkObservationQueries;ctx context.Context;query string;args []any}
func(q watermarkObservationQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{return watermarkObservationRow{q,ctx,query,append([]any(nil),args...)}}
func(r watermarkObservationRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err};q:=r.owner;l:=q.lease
 if q.db==nil||l.Tenant==uuid.Nil||l.Run==uuid.Nil||l.Expansion==uuid.Nil||l.Token==uuid.Nil||q.slot<0||q.slot>=l.SlotCount||q.slot>l.Cursor{return v2input.ErrInvalid}
 count:=4
 switch r.query{
 case `SELECT photo.v30_delivery_stage_result($1,$2,$3,$4)`:
  if q.operation!="derivative.resize"&&q.operation!="derivative.sharpen"{return v2input.ErrInvalid}
 case `SELECT photo.v30_watermark_stage_result($1,$2,$3,$4)`:
  if q.operation!="derivative.watermark"{return v2input.ErrInvalid}
 case `SELECT photo.v30_watermark_saved_continuation_params($1,$2,$3,$4,$5)`:
  count=5
  if q.operation!="derivative.sharpen"&&q.operation!="derivative.watermark"&&q.operation!="export.encode"||domainDigestInvalid(q.paramsSHA){return v2input.ErrInvalid}
 default:return v2input.ErrInvalid
 }
 if len(r.args)!=count{return v2input.ErrInvalid}
 expansion,ok:=r.args[0].(uuid.UUID);if !ok||expansion!=l.Expansion{return v2input.ErrInvalid}
 token,ok:=r.args[1].(uuid.UUID);if !ok||token!=l.Token{return v2input.ErrInvalid}
 slot,ok:=r.args[2].(int);if !ok||slot!=q.slot{return v2input.ErrInvalid}
 operation,ok:=r.args[3].(string);if !ok||operation!=q.operation{return v2input.ErrInvalid}
 if count==5{sha,ok:=r.args[4].(string);if !ok||sha!=q.paramsSHA{return v2input.ErrInvalid}}
 store:=sqlWatermarkEncodeAdmission{db:q.db,lease:l}
 return store.transaction(r.ctx,func(ctx context.Context,tx pgx.Tx)error{return tx.QueryRow(ctx,r.query,r.args...).Scan(dest...)})
}
