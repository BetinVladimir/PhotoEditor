package workflow

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
const watermarkDerivativeInitialActivateQuery=`SELECT photo.activate_v30_watermark_derivative_initial_child($1,$2,$3,$4,$5,$6)`

// InitialDerivativeActivateInTransaction seals and dispatches an already registered initial
// watermark child and commits its parent cursor atomically. Caller owns rollback.
// SQL rechecks current source authority and supports acknowledgement retries.
func(f FrozenDeliveryWatermarkPrefixTask)InitialDerivativeActivateInTransaction(ctx context.Context,tx delivery.ProofTransaction,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent,exportID uuid.UUID)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||exportID!=uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1"))||child.SlotIndex>lease.Cursor{return v2input.ErrInvalid}
 _,raw,err:=f.InitialDerivativeRequest(lease,plan,child);if err!=nil{return err}
 var accepted bool
 err=tx.QueryRow(ctx,watermarkDerivativeInitialActivateQuery,lease.Expansion,lease.Token,child.SlotIndex,string(raw),exportID,string(f.task.closure.ParamsJCS)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("DELIVERY_CHILD_ACTIVATION_REFUSED",409)}
 return nil
}
