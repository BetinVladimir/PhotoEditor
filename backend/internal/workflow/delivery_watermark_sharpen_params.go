package workflow

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// PublishWatermarkSharpenParams binds sharpen to the real verified resize.
// SQL requires its producer receipt, open barrier and current watermark pin.
func PublishWatermarkSharpenParams(ctx context.Context,db delivery.MetadataTransactionBeginner,objects artifactjson.Objects,bucket string,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,source []byte,results []DeliveryPresentationOutput,registry Registry,limits DeliveryDerivativeLimits)(v2input.Ref,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if db==nil||objects==nil||lease.Tenant!=scope.Tenant||lease.Run!=scope.Parent{return v2input.Ref{},v2input.ErrInvalid}
 next,err:=CompileDeliveryWatermarkPrefix(request,pin,scope,source,results,registry,limits);if err!=nil{return v2input.Ref{},err}
 if len(results)!=1||next.Stage!=1||next.Predecessor==uuid.Nil||next.Node.Operation!="derivative.sharpen"{return v2input.Ref{},v2input.ErrInvalid}
 repository:=deliveryParamsRepository{db:watermarkSharpenParamsQueries{db:db,tenant:lease.Tenant},bucket:bucket,lease:lease,slot:request.SlotIndex,owner:next.Node.ID,params:append([]byte(nil),next.Node.Params...)}
 if !repository.valid(repository.scope()){return v2input.Ref{},v2input.ErrInvalid}
 ref,err:=(artifactjson.Publisher{Repository:repository,Objects:objects}).Publish(ctx,repository.scope(),repository.params)
 if err!=nil{return v2input.Ref{},err};return v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},ctx.Err()
}

// Closed query adapter reuses repository validation without granting ordinary
// delivery Params functions access to the watermark encoder path.
type watermarkSharpenParamsQueries struct{db delivery.MetadataTransactionBeginner;tenant uuid.UUID}
type watermarkSharpenParamsRow struct{owner watermarkSharpenParamsQueries;ctx context.Context;query string;args []any}
func(q watermarkSharpenParamsQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{return watermarkSharpenParamsRow{q,ctx,query,append([]any(nil),args...)}}
func(r watermarkSharpenParamsRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err}
 if r.owner.db==nil||r.owner.tenant==uuid.Nil{return v2input.ErrInvalid}
 var query string;index:=0
 switch r.query{
 case `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_child_params($1,$2,$3,$4,$5,$6,$7,$8)`:
  if len(r.args)!=8{return v2input.ErrInvalid};index=5
  query=`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_watermark_sharpen_params($1,$2,$3,$4,$5,$6,$7,$8)`
 case `SELECT photo.complete_v30_delivery_child_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`:
  if len(r.args)!=11{return v2input.ErrInvalid};index=8
  query=`SELECT photo.complete_v30_watermark_sharpen_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
 default:return v2input.ErrInvalid
 }
 tenant,ok:=r.args[index].(uuid.UUID);if !ok||tenant!=r.owner.tenant{return v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(r.ctx,30*time.Second);defer cancel()
 tx,err:=r.owner.db.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(r.ctx),5*time.Second);defer stop();_ =tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{return err}
 if err=tx.QueryRow(ctx,query,r.args...).Scan(dest...);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err};if err=tx.Commit(ctx);err!=nil{return err};return r.ctx.Err()
}
