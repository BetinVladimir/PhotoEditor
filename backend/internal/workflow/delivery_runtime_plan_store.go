package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// DeliveryRuntimePlanStore retains ordinary claim/renew/release fencing while
// loading ordinary/mixed plans from current tenant/project/actor authority.
// No compiler/pin refusal can downgrade to an ordinary plan.
type DeliveryRuntimePlanStore struct{
 delivery.ExpansionLeaseStore
 Transactions delivery.MetadataTransactionBeginner
}
func(s DeliveryRuntimePlanStore)LoadPlan(ctx context.Context,lease delivery.ExpansionLease)(delivery.PreparePlan,error){
 if err:=ctx.Err();err!=nil{return delivery.PreparePlan{},err}
 if s.Transactions==nil||!lease.Valid(){return delivery.PreparePlan{},v2input.ErrInvalid}
 installation:=sqlWatermarkEncodeAdmission{db:s.Transactions,lease:lease}
 if err:=installation.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{return delivery.ValidateWatermarkSchedulerInstallation(ctx,tx)});err!=nil{return delivery.PreparePlan{},err}
 plan,_,err:=delivery.LoadPersistedWatermarkPlan(ctx,s.Transactions,lease)
 return plan,err
}
var _ DeliveryExpansionStore=DeliveryRuntimePlanStore{}
