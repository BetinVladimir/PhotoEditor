package workflow

import (
 "bytes"
 "context"
 "time"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
// AdmitWatermarkAfterPresentation resumes acknowledged activation or committed input
// registration before publishing anything. No SQL transaction spans object IO.
func(f FrozenDeliveryWatermarkPrefixTask)AdmitWatermarkAfterPresentation(ctx context.Context,db delivery.MetadataTransactionBeginner,publisher v2input.Publisher,lease delivery.ExpansionLease,request delivery.FrozenExportRequest)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 raw,err:=canonical.Marshal(request);if err!=nil{return false,err}
 if db==nil||publisher==nil||lease.Tenant!=f.scope.Tenant||lease.Run!=request.ParentRunID||!bytes.Equal(raw,f.request){return false,v2input.ErrInvalid}
 installation:=sqlWatermarkPresentationAdmission{db:db,lease:lease}
 if err=installation.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{return delivery.ValidateWatermarkSchedulerInstallation(ctx,tx)});err!=nil{return false,err}
 return f.admitWatermarkPresentation(ctx,sqlWatermarkPresentationAdmission{db:db,frozen:f,lease:lease,request:request},publisher)
}
func (f FrozenDeliveryWatermarkPrefixTask) admitWatermarkPresentation(ctx context.Context, store continuationAdmissionStore, publisher v2input.Publisher) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if store == nil || publisher == nil {
		return false, v2input.ErrInvalid
	}
	if _, err := f.DraftTask(); err != nil {
		return false, err
	}
	activated, err := store.Activated(ctx)
	if err != nil || activated {
		return activated, err
	}
	registered, err := store.Registered(ctx)
	if err != nil {
		return false, err
	}
	if registered {
		return false, store.Commit(ctx, nil)
	}
	if err := store.Create(ctx); err != nil {
		return false, err
	}
	graph, err := f.PublishInputs(ctx, publisher)
	if err != nil {
		return false, err
	}
	return false, store.Commit(ctx, &graph)
}

// All receipts require tenant context too. Read probes intentionally use short
// writable transactions because current source authorization takes row locks.
type sqlWatermarkPresentationAdmission struct{db delivery.MetadataTransactionBeginner;frozen FrozenDeliveryWatermarkPrefixTask;lease delivery.ExpansionLease;request delivery.FrozenExportRequest}
func(s sqlWatermarkPresentationAdmission)transaction(ctx context.Context,fn func(context.Context,pgx.Tx)error)error{
 bounded,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
 tx,err:=s.db.BeginTx(bounded,pgx.TxOptions{});if err!=nil{return err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer stop();_=tx.Rollback(cleanup)}()
 if _,err=tx.Exec(bounded,`SELECT set_config('app.tenant_id',$1,true)`,s.lease.Tenant.String());err!=nil{return err}
 if err=fn(bounded,tx);err!=nil{return err};if err=bounded.Err();err!=nil{return err}
 if err=tx.Commit(bounded);err!=nil{return err};return ctx.Err()
}
func(s sqlWatermarkPresentationAdmission)Activated(ctx context.Context)(bool,error){
 var ready bool
 err:=s.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{var err error;ready,err=s.frozen.ActivationRegistered(ctx,tx,s.lease,s.request);return err})
 if err!=nil{return false,err};return ready,nil
}
func(s sqlWatermarkPresentationAdmission)Registered(ctx context.Context)(bool,error){
 var ready bool
 err:=s.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{var err error;ready,err=s.frozen.TransportRegistered(ctx,tx,s.lease,s.request);return err})
 if err!=nil{return false,err};return ready,nil
}
func(s sqlWatermarkPresentationAdmission)Create(ctx context.Context)error{
 return s.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  ready,err:=s.frozen.ActivationRegistered(ctx,tx,s.lease,s.request);if err!=nil||ready{return err}
  ready,err=s.frozen.TransportRegistered(ctx,tx,s.lease,s.request);if err!=nil||ready{return err}
  return s.frozen.CreateInTransaction(ctx,tx,s.lease,s.request)
 })
}
func(s sqlWatermarkPresentationAdmission)Commit(ctx context.Context,graph *v2input.GraphTask)error{
 return s.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  ready,err:=s.frozen.ActivationRegistered(ctx,tx,s.lease,s.request);if err!=nil||ready{return err}
  ready,err=s.frozen.TransportRegistered(ctx,tx,s.lease,s.request);if err!=nil{return err}
  if !ready{if graph==nil{return v2input.ErrInvalid};if err=s.frozen.RegisterInputsInTransaction(ctx,tx,s.lease,s.request,*graph);err!=nil{return err}}
  return s.frozen.ActivateInTransaction(ctx,tx,s.lease,s.request)
 })
}
