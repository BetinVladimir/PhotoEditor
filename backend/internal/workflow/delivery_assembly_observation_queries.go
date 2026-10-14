package workflow

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// Each matrix read commits before publication can perform object IO.
// This adapter grants only the exact claimed expansion's observation and renewal.
type deliveryAssemblyObservationQueries struct {
 db delivery.MetadataTransactionBeginner
 lease delivery.ExpansionLease
}
type deliveryAssemblyObservationRow struct {
 owner deliveryAssemblyObservationQueries
 ctx context.Context
 query string
 args []any
}
func(q deliveryAssemblyObservationQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{
 return deliveryAssemblyObservationRow{q,ctx,query,append([]any(nil),args...)}
}
func(r deliveryAssemblyObservationRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err}
 l:=r.owner.lease
 if r.owner.db==nil||!l.Valid(){return v2input.ErrInvalid}
 switch r.query {
 case `SELECT photo.v30_delivery_observation_page($1,$2,$3,$4)`:
  if len(r.args)!=4{return v2input.ErrInvalid}
  cursor,ok:=r.args[2].(int);limit,good:=r.args[3].(int)
  if !ok||!good||cursor<0||cursor>l.Cursor||limit<1||limit>100{return v2input.ErrInvalid}
 case `SELECT photo.update_v30_delivery_expansion_lease($1,$2,$3)`:
  if len(r.args)!=3{return v2input.ErrInvalid}
  release,ok:=r.args[2].(bool);if !ok||release{return v2input.ErrInvalid}
 default:return v2input.ErrInvalid
 }
 expansion,ok:=r.args[0].(uuid.UUID);token,good:=r.args[1].(uuid.UUID)
 if !ok||!good||expansion!=l.Expansion||token!=l.Token{return v2input.ErrInvalid}
 return (sqlWatermarkEncodeAdmission{db:r.owner.db,lease:l}).transaction(r.ctx,func(ctx context.Context,tx pgx.Tx)error{
  return tx.QueryRow(ctx,r.query,r.args...).Scan(dest...)
 })
}
