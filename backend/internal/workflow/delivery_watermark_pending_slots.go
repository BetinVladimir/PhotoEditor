package workflow

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

var _ DeliveryChildObserver=DeliveryWatermarkChildPipeline{}

func(p DeliveryWatermarkChildPipeline)PendingSlots(ctx context.Context,lease delivery.ExpansionLease,limit int)([]int,error){
 return readWatermarkPendingSlots(ctx,p.DB,lease,limit)
}
// Discovery is not physical verification or adoption. Return only after the
// tenant transaction commits, so callers can perform object IO without locks.
func readWatermarkPendingSlots(ctx context.Context,db delivery.MetadataTransactionBeginner,lease delivery.ExpansionLease,limit int)([]int,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if db==nil||lease.Tenant==uuid.Nil||lease.Run==uuid.Nil||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||limit<1||limit>100||lease.Cursor<0||lease.Cursor>lease.SlotCount{return nil,v2input.ErrInvalid}
 var slots []int
 store:=sqlWatermarkEncodeAdmission{db:db,lease:lease}
 err:=store.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  var raw []byte
  if err:=tx.QueryRow(ctx,`SELECT photo.v30_watermark_pending_child_slots($1,$2,$3)`,lease.Expansion,lease.Token,limit).Scan(&raw);err!=nil{return err}
  var err error;slots,err=parseDeliveryPendingSlots(raw,lease.Cursor,limit);return err
 })
 if err!=nil{return nil,err};return slots,nil
}
