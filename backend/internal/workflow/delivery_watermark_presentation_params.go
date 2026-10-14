package workflow

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// PublishPresentationWatermarkParams requires the full real presentation prefix.
// SQL independently binds both publication phases to the current verified
// producer and persisted watermark pin. No object IO occurs inside a SQL TX.
func PublishPresentationWatermarkParams(ctx context.Context,db delivery.MetadataTransactionBeginner,objects artifactjson.Objects,bucket string,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,source []byte,results []DeliveryPresentationOutput,registry Registry,limits DeliveryDerivativeLimits)(v2input.Ref,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if db==nil||objects==nil||lease.Tenant!=scope.Tenant||lease.Run!=scope.Parent||len(results)<1||len(results)>2{return v2input.Ref{},v2input.ErrInvalid}
 next,err:=CompileDeliveryWatermarkPrefix(request,pin,scope,source,results,registry,limits);if err!=nil{return v2input.Ref{},err}
 if next.Node.Operation!="derivative.watermark"||next.Stage!=len(results)||next.Predecessor==uuid.Nil{return v2input.Ref{},v2input.ErrInvalid}
 repository:=deliveryParamsRepository{db:watermarkInitialParamsQueries{db:db,tenant:lease.Tenant,image:append([]byte(nil),next.SourceJCS...),afterPresentation:true},bucket:bucket,lease:lease,slot:request.SlotIndex,owner:next.Node.ID,params:append([]byte(nil),next.Node.Params...)}
 if !repository.valid(repository.scope()){return v2input.Ref{},v2input.ErrInvalid}
 ref,err:=(artifactjson.Publisher{Repository:repository,Objects:objects}).Publish(ctx,repository.scope(),repository.params);if err!=nil{return v2input.Ref{},err}
 return v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},ctx.Err()
}
