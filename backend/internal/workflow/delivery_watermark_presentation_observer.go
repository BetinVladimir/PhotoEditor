package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

type watermarkPresentationProgress struct{
 prefix []DeliveryPresentationOutput
 entries []v2input.Entry
 admitted bool
}

// advancePresentation reconstructs only physically verified resize/sharpen.
// Recovery is checked before barrier mutation; no object IO spans a SQL TX.
// Reaching watermark returns the prefix for subsequent watermark/encode handling.
func(p DeliveryWatermarkChildPipeline)advancePresentation(ctx context.Context,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent,source DeliveryResolvedWatermarkChild)(watermarkPresentationProgress,error){
 result:=watermarkPresentationProgress{entries:append([]v2input.Entry(nil),source.Entries...)}
 if err:=ctx.Err();err!=nil{return result,err}
 if p.DB==nil||p.Objects==nil||p.Reader==nil{return result,v2input.ErrInvalid}
 request,_,err:=plan.WatermarkExportRequest(lease.Run,child,source.Scope,source.Pin);if err!=nil{return result,err}
 pins,err:=delivery.NewWatermarkPinStore(p.DB,lease.Tenant);if err!=nil{return result,err}
 for len(result.prefix)<2{
  pending,err:=CompileDeliveryWatermarkPrefix(request,source.Pin,source.Scope,source.ImageJCS,result.prefix,p.Registry,p.Limits);if err!=nil{return result,err}
  if pending.Node.Operation=="derivative.watermark"{return result,nil}
  queries:=watermarkObservationQueries{db:p.DB,lease:lease,slot:child.SlotIndex,operation:pending.Node.Operation}
  verified,err:=LoadVerifiedWatermarkPresentationStage(ctx,queries,pins,p.Reader,p.Bucket,lease,request,source.Scope,source.ImageJCS,result.prefix,p.Registry,p.Limits)
  if err!=nil||verified==nil{return result,err}
  result.prefix=append(result.prefix,verified.Output())
  result.entries,err=deliveryStageEntries(result.entries,verified.Producer());if err!=nil{return result,err}
  next,err:=CompileDeliveryWatermarkPrefix(request,source.Pin,source.Scope,source.ImageJCS,result.prefix,p.Registry,p.Limits);if err!=nil{return result,err}
  recovery:=watermarkObservationQueries{db:p.DB,lease:lease,slot:child.SlotIndex,operation:next.Node.Operation,paramsSHA:renderDerivativeBytesSHA(next.Node.Params)}
  saved,err:=ReadSavedWatermarkContinuationParams(ctx,recovery,lease,child,next.Node);if err!=nil{return result,err}
  freeze:=func(ref v2input.Ref)(FrozenDeliveryWatermarkPrefixTask,error){return FreezeDeliveryWatermarkPrefixTask(request,source.Pin,source.Scope,source.ImageJCS,result.prefix,p.Registry,p.Limits,ref,result.entries)}
  documents:=artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:p.DB,Bucket:p.Bucket},Objects:p.Objects}
  publisher:=v2input.ScopedPublisher{Documents:documents,Tenant:lease.Tenant,Run:child.RunID}
  actions:=deliveryContinuationActions{
   activated:func(ref v2input.Ref)(bool,error){
    frozen,err:=freeze(ref);if err!=nil{return false,err}
    if next.Node.Operation=="derivative.sharpen"{return (sqlWatermarkSharpenAdmission{db:p.DB,frozen:frozen,lease:lease,request:request}).Activated(ctx)}
    return (sqlWatermarkPresentationAdmission{db:p.DB,frozen:frozen,lease:lease,request:request}).Activated(ctx)
   },
   open:func()error{
    store:=sqlWatermarkEncodeAdmission{db:p.DB,lease:lease}
    return store.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
     if next.Node.Operation=="derivative.sharpen"{return verified.BeginSharpenInTransaction(ctx,tx,lease,request,p.Bucket)}
     return verified.BeginWatermarkInTransaction(ctx,tx,lease,request,p.Bucket)
    })
   },
   publish:func()(v2input.Ref,error){
    if next.Node.Operation=="derivative.sharpen"{return PublishWatermarkSharpenParams(ctx,p.DB,p.Objects,p.Bucket,lease,request,source.Pin,source.Scope,source.ImageJCS,result.prefix,p.Registry,p.Limits)}
    return PublishPresentationWatermarkParams(ctx,p.DB,p.Objects,p.Bucket,lease,request,source.Pin,source.Scope,source.ImageJCS,result.prefix,p.Registry,p.Limits)
   },
   admit:func(ref v2input.Ref)(bool,error){
    frozen,err:=freeze(ref);if err!=nil{return false,err}
    if next.Node.Operation=="derivative.sharpen"{return frozen.AdmitWatermarkSharpen(ctx,p.DB,publisher,lease,request)}
    return frozen.AdmitWatermarkAfterPresentation(ctx,p.DB,publisher,lease,request)
   },
  }
  recovered,err:=resumeDeliveryContinuation(ctx,saved,actions);if err!=nil{return result,err}
  if !recovered{result.admitted=true;return result,nil}
  if next.Node.Operation=="derivative.watermark"{return result,nil}
 }
 return result,nil
}
