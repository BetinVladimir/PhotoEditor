package workflow
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
const watermarkInitialActivatedQuery=`SELECT photo.v30_watermark_initial_child_activated($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`

// ActivationRegistered reads the committed initial admission under current source authority.
func(f FrozenDeliveryWatermarkTask)ActivationRegistered(ctx context.Context,db delivery.ProofTransaction,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent,exportID uuid.UUID)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if db==nil||exportID!=uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1")){return false,v2input.ErrInvalid}
 _,raw,err:=f.InitialRequest(lease,plan,child);if err!=nil{return false,err}
 task,err:=canonical.Marshal(f.task.draft);if err!=nil{return false,err}
 var ready bool
 err=db.QueryRow(ctx,watermarkInitialActivatedQuery,lease.Expansion,lease.Token,child.SlotIndex,string(raw),exportID,string(task),string(f.task.closure.ParamsJCS),string(f.image)).Scan(&ready)
 if err!=nil{return false,err};if err=ctx.Err();err!=nil{return false,err};return ready,nil
}

// InitialDerivativeActivationRegistered reads the committed initial admission under current source authority.
func(f FrozenDeliveryWatermarkPrefixTask)InitialDerivativeActivationRegistered(ctx context.Context,db delivery.ProofTransaction,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent,exportID uuid.UUID)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if db==nil||exportID!=uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1")){return false,v2input.ErrInvalid}
 _,raw,err:=f.InitialDerivativeRequest(lease,plan,child);if err!=nil{return false,err}
 task,err:=canonical.Marshal(f.task.draft);if err!=nil{return false,err}
 var ready bool
 err=db.QueryRow(ctx,watermarkInitialActivatedQuery,lease.Expansion,lease.Token,child.SlotIndex,string(raw),exportID,string(task),string(f.task.closure.ParamsJCS),string(f.source)).Scan(&ready)
 if err!=nil{return false,err};if err=ctx.Err();err!=nil{return false,err};return ready,nil
}
