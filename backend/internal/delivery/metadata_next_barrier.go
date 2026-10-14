package delivery

import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type MetadataSchedulerDB interface{MetadataTransactionBeginner;ProofTransaction}
type MetadataNextBarrierStore struct{DB MetadataSchedulerDB}
func(s MetadataNextBarrierStore)Next(ctx context.Context)(*MetadataBarrierLease,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.DB==nil{return nil,v2input.ErrInvalid}
 var lease MetadataBarrierLease
 err:=s.DB.QueryRow(ctx,`SELECT tenant_id,child_run_id,expansion_id,lease_token,lease_until,parent_task_id FROM photo.claim_next_v30_metadata_barrier()`).Scan(
 &lease.Tenant,&lease.ChildRun,&lease.Expansion,&lease.Token,&lease.Until,&lease.ParentTask)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil}
 if err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err}
 if !lease.valid(){return nil,v2input.ErrInvalid}
 return &lease,nil
}
func(s MetadataNextBarrierStore)Release(ctx context.Context,lease MetadataBarrierLease)error{
 store,err:=NewMetadataBarrierLeaseStore(s.DB,lease.Tenant);if err!=nil{return err}
 return store.Release(ctx,lease)
}
