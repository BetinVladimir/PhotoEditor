package workflow

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

const watermarkDerivativeInitialTransportQuery=`SELECT photo.v30_watermark_derivative_initial_transport_ready($1,$2,$3,$4::jsonb,$5)`

// InitialDerivativeTransportRegistered acknowledges a blocked task only under the current pin
// and expansion fence. It does not acknowledge activation or completion.
func(f FrozenDeliveryWatermarkPrefixTask)InitialDerivativeTransportRegistered(ctx context.Context,db delivery.ProofTransaction,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if db==nil{return false,v2input.ErrInvalid}
 if _,_,err:=f.InitialDerivativeRequest(lease,plan,child);err!=nil{return false,err}
 task,err:=canonical.Marshal(f.task.draft);if err!=nil{return false,err}
 var ready bool
 err=db.QueryRow(ctx,watermarkDerivativeInitialTransportQuery,lease.Expansion,lease.Token,child.SlotIndex,string(task),string(f.task.closure.ParamsJCS)).Scan(&ready)
 if err!=nil{return false,err};if err=ctx.Err();err!=nil{return false,err}
 return ready,nil
}

// RegisterInitialDerivativeInputsInTransaction attaches only the previously published frozen
// closure. Caller must roll back every error; no task is sealed or dispatched.
func(f FrozenDeliveryWatermarkPrefixTask)RegisterInitialDerivativeInputsInTransaction(ctx context.Context,tx pgx.Tx,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent,exportID uuid.UUID,reservedBytes int64,graph v2input.GraphTask)error{
 if err:=ctx.Err();err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 draft,err:=f.DraftTask();if err!=nil{return err};if graph.ID!=draft.ID{return v2input.ErrInvalid}
 ready,err:=f.InitialDerivativeTransportRegistered(ctx,tx,lease,plan,child);if err!=nil||ready{return err}
 if err=f.CreateDraft(ctx,tx,lease,plan,child,exportID,reservedBytes);err!=nil{return err}
 return v2input.RegisterInTransaction(ctx,tx,lease.Tenant,child.RunID,draft.ID,graph.Built,f.task.closure.Open,f.task.closure.Direct)
}
