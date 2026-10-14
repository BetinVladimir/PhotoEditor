package workflow

import (
 "context"
 "time"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// PublishInitialWatermarkParams publishes only a first watermark task, never
// skips pending resize/sharpen. Exact source bytes accompany both SQL phases.
func PublishInitialWatermarkParams(ctx context.Context,db delivery.MetadataTransactionBeginner,objects artifactjson.Objects,bucket string,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,source []byte,registry Registry,limits DeliveryDerivativeLimits)(v2input.Ref,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if db==nil||objects==nil||lease.Tenant!=scope.Tenant||lease.Run!=scope.Parent{return v2input.Ref{},v2input.ErrInvalid}
 owned:=append([]byte(nil),source...)
 next,err:=CompileDeliveryWatermarkPrefix(request,pin,scope,owned,nil,registry,limits);if err!=nil{return v2input.Ref{},err}
 if next.Node.Operation!="derivative.watermark"||next.Stage!=0||next.Predecessor!=uuid.Nil{return v2input.Ref{},v2input.ErrInvalid}
 repository:=deliveryParamsRepository{db:watermarkInitialParamsQueries{db:db,tenant:lease.Tenant,image:owned},bucket:bucket,lease:lease,slot:request.SlotIndex,owner:next.Node.ID,params:append([]byte(nil),next.Node.Params...)}
 if !repository.valid(repository.scope()){return v2input.Ref{},v2input.ErrInvalid}
 ref,err:=(artifactjson.Publisher{Repository:repository,Objects:objects}).Publish(ctx,repository.scope(),repository.params);if err!=nil{return v2input.Ref{},err}
 return v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},ctx.Err()
}
// Closed query adapter reuses repository validation without granting ordinary
// delivery Params functions access to the watermark encoder path.
type watermarkInitialParamsQueries struct{db delivery.MetadataTransactionBeginner;tenant uuid.UUID;image []byte;afterPresentation bool}
type watermarkInitialParamsRow struct{owner watermarkInitialParamsQueries;ctx context.Context;query string;args []any}
func(q watermarkInitialParamsQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{return watermarkInitialParamsRow{q,ctx,query,append([]any(nil),args...)}}
func(r watermarkInitialParamsRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err}
 if r.owner.db==nil||r.owner.tenant==uuid.Nil||len(r.owner.image)<2||len(r.owner.image)>1<<20{return v2input.ErrInvalid}
 var query string;index:=0
 switch r.query{
 case `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_child_params($1,$2,$3,$4,$5,$6,$7,$8)`:
  if len(r.args)!=8{return v2input.ErrInvalid};index=5
  query=`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_watermark_initial_params($1,$2,$3,$4,$5,$6,$7,$8,$9)`
 case `SELECT photo.complete_v30_delivery_child_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`:
  if len(r.args)!=11{return v2input.ErrInvalid};index=8
  query=`SELECT photo.complete_v30_watermark_initial_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
 default:return v2input.ErrInvalid
 }
 if r.owner.afterPresentation{query=strings.Replace(query,"_watermark_initial_params(","_watermark_presentation_params(",1)}
 tenant,ok:=r.args[index].(uuid.UUID);if !ok||tenant!=r.owner.tenant{return v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(r.ctx,30*time.Second);defer cancel()
 tx,err:=r.owner.db.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(r.ctx),5*time.Second);defer stop();_ =tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{return err}
 if err=tx.QueryRow(ctx,query,append(r.args,string(r.owner.image))...).Scan(dest...);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err};if err=tx.Commit(ctx);err!=nil{return err};return r.ctx.Err()
}
