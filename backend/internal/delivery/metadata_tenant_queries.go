package delivery

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

type MetadataTransactionBeginner interface {BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error)}

// MetadataTenantQueries commits every SQL call before Scan returns, so the
// immutable publisher cannot retain SQL locks while performing object IO.
// DB must authenticate as photo_scheduler; no SET ROLE elevation is performed.
type MetadataTenantQueries struct {DB MetadataTransactionBeginner;Tenant uuid.UUID}
type metadataTenantRow struct {owner MetadataTenantQueries;ctx context.Context;query string;args []any}
func(q MetadataTenantQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{
 return metadataTenantRow{owner:q,ctx:ctx,query:query,args:append([]any(nil),args...)}
}
func(r metadataTenantRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err}
 if r.owner.DB==nil||r.owner.Tenant==uuid.Nil||len(r.args)==0{return v2input.ErrInvalid}
 tenant,ok:=r.args[0].(uuid.UUID)
 if !ok||tenant!=r.owner.Tenant{return v2input.ErrInvalid}
 switch r.query{
 case metadataInventoryEntryQuery,watermarkPinReadQuery,watermarkExpansionScopeQuery:

 case `SELECT child_config,encoded_artifact,encoded_jcs,encoded_bytes FROM photo.read_v30_metadata_stage_input($1,$2,$3,$4)`,
 `SELECT publication_jcs,publication_sha FROM photo.read_v30_metadata_source($1,$2,$3,$4,$5,$6)`,
 `SELECT photo.pin_v30_delivery_metadata_proof($1,$2,$3,$4,$5)`,
 `SELECT proof_jcs,proof_sha FROM photo.read_v30_delivery_metadata_proof($1,$2,$3)`,
 `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_metadata_document($1,$2,$3,$4,$5,$6)`,
 `SELECT photo.complete_v30_metadata_document($1,$2,$3,$4,$5,$6,$7,$8)`,
 `SELECT photo.create_v30_metadata_task($1,$2,$3,$4,$5)`,
 `SELECT photo.v30_metadata_transport_ready($1,$2,$3,$4)`,
 `SELECT photo.activate_v30_metadata_task($1,$2,$3,$4)`,
 `SELECT photo.v30_metadata_task_activated($1,$2,$3,$4)`,
 `SELECT expansion_id,lease_token,lease_until,parent_task_id FROM photo.claim_v30_metadata_barrier($1,$2)`,
 `SELECT photo.update_v30_metadata_barrier_lease($1,$2,$3,$4,$5)`:
 default:return v2input.ErrInvalid
 }
 ctx,cancel:=context.WithTimeout(r.ctx,30*time.Second);defer cancel()
 tx,err:=r.owner.DB.BeginTx(ctx,pgx.TxOptions{})
 if err!=nil{return err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(r.ctx),5*time.Second);defer stop();_ =tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{return err}
 if err=tx.QueryRow(ctx,r.query,r.args...).Scan(dest...);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 if err=tx.Commit(ctx);err!=nil{return err}
 return r.ctx.Err()
}

func NewMetadataProofStore(db MetadataTransactionBeginner,tenant uuid.UUID)(SQLMetadataProofStore,error){
 if db==nil||tenant==uuid.Nil{return SQLMetadataProofStore{},v2input.ErrInvalid}
 return SQLMetadataProofStore{DB:MetadataTenantQueries{DB:db,Tenant:tenant}},nil
}
func NewMetadataDocumentRepository(db MetadataTransactionBeginner,scope MetadataProofScope,plan MetadataWritePlan,bucket string)(MetadataDocumentRepository,error){
 if db==nil||scope.Tenant==uuid.Nil{return MetadataDocumentRepository{},v2input.ErrInvalid}
 scope.ApprovedFields=append([]string(nil),scope.ApprovedFields...)
 owned:=MetadataWritePlan{params:plan.Params(),projection:plan.Projection(),inputs:plan.Inputs(),paramsJCS:plan.ParamsJCS(),projectionJCS:plan.ProjectionJCS()}
 repository:=MetadataDocumentRepository{DB:MetadataTenantQueries{DB:db,Tenant:scope.Tenant},Scope:scope,Plan:owned,Bucket:bucket}
 for _,purpose:=range []string{"v30_metadata_projection","task_params"}{
  if _,_,err:=repository.document(metadataPublicationScope(scope,purpose));err!=nil{return MetadataDocumentRepository{},err}
 }
 return repository,nil
}
