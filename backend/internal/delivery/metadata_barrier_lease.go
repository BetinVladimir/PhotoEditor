package delivery

import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type MetadataBarrierLease struct{Tenant,ChildRun,Expansion,Token,ParentTask uuid.UUID;Until time.Time}
type MetadataBarrierLeaseStore struct{DB ProofTransaction}
func NewMetadataBarrierLeaseStore(db MetadataTransactionBeginner,tenant uuid.UUID)(MetadataBarrierLeaseStore,error){
 if db==nil||tenant==uuid.Nil{return MetadataBarrierLeaseStore{},v2input.ErrInvalid}
 return MetadataBarrierLeaseStore{DB:MetadataTenantQueries{DB:db,Tenant:tenant}},nil
}
func(s MetadataBarrierLeaseStore)Claim(ctx context.Context,tenant,child uuid.UUID)(*MetadataBarrierLease,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.DB==nil||tenant==uuid.Nil||child==uuid.Nil{return nil,v2input.ErrInvalid}
 lease:=MetadataBarrierLease{Tenant:tenant,ChildRun:child}
 err:=s.DB.QueryRow(ctx,`SELECT expansion_id,lease_token,lease_until,parent_task_id FROM photo.claim_v30_metadata_barrier($1,$2)`,tenant,child).Scan(&lease.Expansion,&lease.Token,&lease.Until,&lease.ParentTask)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err}
 if !lease.valid(){return nil,v2input.ErrInvalid}
 return &lease,nil
}
func(l MetadataBarrierLease)valid()bool{return l.Tenant!=uuid.Nil&&l.ChildRun!=uuid.Nil&&l.Expansion!=uuid.Nil&&l.Token!=uuid.Nil&&l.ParentTask!=uuid.Nil&&!l.Until.IsZero()}
func(s MetadataBarrierLeaseStore)Renew(ctx context.Context,lease MetadataBarrierLease)error{return s.update(ctx,lease,false)}
func(s MetadataBarrierLeaseStore)Release(ctx context.Context,lease MetadataBarrierLease)error{return s.update(ctx,lease,true)}
func(s MetadataBarrierLeaseStore)update(ctx context.Context,lease MetadataBarrierLease,release bool)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||!lease.valid(){return v2input.ErrInvalid}
 var accepted bool
 err:=s.DB.QueryRow(ctx,`SELECT photo.update_v30_metadata_barrier_lease($1,$2,$3,$4,$5)`,lease.Tenant,lease.ChildRun,lease.Expansion,lease.Token,release).Scan(&accepted)
 if err!=nil{return err};if err=ctx.Err();err!=nil{return err}
 if !accepted{return domain.NewError("DELIVERY_METADATA_STALE",409)}
 return nil
}
