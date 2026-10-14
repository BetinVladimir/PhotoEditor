package workflow

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

type watermarkInitialAdmissionTask interface{
 DraftTask()(v2input.DraftTask,error)
 PublishInputs(context.Context,v2input.Publisher)(v2input.GraphTask,error)
 CreateDraft(context.Context,delivery.ProofTransaction,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent,uuid.UUID,int64)error
 ActivationRegistered(context.Context,delivery.ProofTransaction,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent,uuid.UUID)(bool,error)
 TransportRegistered(context.Context,delivery.ProofTransaction,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent)(bool,error)
 RegisterInputsInTransaction(context.Context,pgx.Tx,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent,uuid.UUID,int64,v2input.GraphTask)error
 ActivateInTransaction(context.Context,delivery.ProofTransaction,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent,uuid.UUID)error
}

type watermarkInitialDerivativeAdapter struct{FrozenDeliveryWatermarkPrefixTask}
func(f watermarkInitialDerivativeAdapter)ActivationRegistered(ctx context.Context,db delivery.ProofTransaction,l delivery.ExpansionLease,p delivery.PreparePlan,c delivery.ExportChildIntent,id uuid.UUID)(bool,error){return f.InitialDerivativeActivationRegistered(ctx,db,l,p,c,id)}
func(f watermarkInitialDerivativeAdapter)TransportRegistered(ctx context.Context,db delivery.ProofTransaction,l delivery.ExpansionLease,p delivery.PreparePlan,c delivery.ExportChildIntent)(bool,error){return f.InitialDerivativeTransportRegistered(ctx,db,l,p,c)}
func(f watermarkInitialDerivativeAdapter)RegisterInputsInTransaction(ctx context.Context,tx pgx.Tx,l delivery.ExpansionLease,p delivery.PreparePlan,c delivery.ExportChildIntent,id uuid.UUID,reserved int64,g v2input.GraphTask)error{return f.RegisterInitialDerivativeInputsInTransaction(ctx,tx,l,p,c,id,reserved,g)}
func(f watermarkInitialDerivativeAdapter)ActivateInTransaction(ctx context.Context,db delivery.ProofTransaction,l delivery.ExpansionLease,p delivery.PreparePlan,c delivery.ExportChildIntent,id uuid.UUID)error{return f.InitialDerivativeActivateInTransaction(ctx,db,l,p,c,id)}

func(f FrozenDeliveryWatermarkTask)AdmitInitial(ctx context.Context,db delivery.MetadataTransactionBeginner,publisher v2input.Publisher,l delivery.ExpansionLease,p delivery.PreparePlan,c delivery.ExportChildIntent,id uuid.UUID,reserved int64)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if _,_,err:=f.InitialRequest(l,p,c);err!=nil{return false,err}
 return admitInitialWatermark(ctx,db,publisher,f,l,p,c,id,reserved)
}
func(f FrozenDeliveryWatermarkPrefixTask)AdmitInitialDerivative(ctx context.Context,db delivery.MetadataTransactionBeginner,publisher v2input.Publisher,l delivery.ExpansionLease,p delivery.PreparePlan,c delivery.ExportChildIntent,id uuid.UUID,reserved int64)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if _,_,err:=f.InitialDerivativeRequest(l,p,c);err!=nil{return false,err}
 return admitInitialWatermark(ctx,db,publisher,watermarkInitialDerivativeAdapter{f},l,p,c,id,reserved)
}

// admitInitialWatermark never keeps a SQL transaction open across object IO.
// A true return means an earlier committed activation was acknowledged.
func admitInitialWatermark(ctx context.Context,db delivery.MetadataTransactionBeginner,publisher v2input.Publisher,task watermarkInitialAdmissionTask,l delivery.ExpansionLease,p delivery.PreparePlan,c delivery.ExportChildIntent,id uuid.UUID,reserved int64)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if db==nil||publisher==nil||task==nil||l.Tenant==uuid.Nil||reserved<1||reserved>9007199254740991||id!=uuid.NewSHA1(c.RunID,[]byte("v30:delivery-export:1")){return false,v2input.ErrInvalid}
 transaction:=func(fn func(context.Context,pgx.Tx)error)error{
  bounded,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
  tx,err:=db.BeginTx(bounded,pgx.TxOptions{});if err!=nil{return err}
  defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer stop();_=tx.Rollback(cleanup)}()
  if _,err=tx.Exec(bounded,`SELECT set_config('app.tenant_id',$1,true)`,l.Tenant.String());err!=nil{return err}
  if err=fn(bounded,tx);err!=nil{return err};if err=bounded.Err();err!=nil{return err}
  if err=tx.Commit(bounded);err!=nil{return err};return ctx.Err()
 }
 if err:=transaction(func(ctx context.Context,tx pgx.Tx)error{return delivery.ValidateWatermarkSchedulerInstallation(ctx,tx)});err!=nil{return false,err}
 var activated,registered bool
 err:=transaction(func(ctx context.Context,tx pgx.Tx)error{
  var err error
  activated,err=task.ActivationRegistered(ctx,tx,l,p,c,id);if err!=nil||activated{return err}
  registered,err=task.TransportRegistered(ctx,tx,l,p,c);if err!=nil||registered{return err}
  return task.CreateDraft(ctx,tx,l,p,c,id,reserved)
 })
 if err!=nil||activated{return activated,err}
 var graph *v2input.GraphTask
 if !registered{published,err:=task.PublishInputs(ctx,publisher);if err!=nil{return false,err};graph=&published}
 err=transaction(func(ctx context.Context,tx pgx.Tx)error{
  ready,err:=task.ActivationRegistered(ctx,tx,l,p,c,id);if err!=nil||ready{return err}
  ready,err=task.TransportRegistered(ctx,tx,l,p,c);if err!=nil{return err}
  if !ready{if graph==nil{return v2input.ErrInvalid};if err=task.RegisterInputsInTransaction(ctx,tx,l,p,c,id,reserved,*graph);err!=nil{return err}}
  return task.ActivateInTransaction(ctx,tx,l,p,c,id)
 })
 return false,err
}
