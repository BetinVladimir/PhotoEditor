package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// ObserveChild reconstructs presentation and watermark from verified physical
// outputs, then admits encode. It reports new admission, not export completion.
// Pending-slot discovery and assembly remain independent integration contracts.
func(p DeliveryWatermarkChildPipeline)ObserveChild(ctx context.Context,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if p.DB==nil||p.Objects==nil||p.Reader==nil||p.Resolver==nil||!lease.Valid()||lease.ParentTask!=plan.TaskID()||child.SlotIndex<0||child.SlotIndex>=lease.SlotCount{return false,v2input.ErrInvalid}
 source,err:=p.Resolver.ResolveWatermarkChild(ctx,lease,plan,child);if err!=nil{return false,err}
 source.ImageJCS=append([]byte(nil),source.ImageJCS...)
 source.Entries=append([]v2input.Entry(nil),source.Entries...)
 if domainDigestInvalid(source.TargetICCSHA){return false,v2input.ErrInvalid}
 request,_,err:=plan.WatermarkExportRequest(lease.Run,child,source.Scope,source.Pin);if err!=nil{return false,err}
 presentation,err:=p.advancePresentation(ctx,lease,plan,child,source);if err!=nil||presentation.admitted{return presentation.admitted,err}
 pending,err:=CompileDeliveryWatermarkPrefix(request,source.Pin,source.Scope,source.ImageJCS,presentation.prefix,p.Registry,p.Limits);if err!=nil{return false,err}
 // A required derivative has not produced a verified result yet.
 if pending.Node.Operation!="derivative.watermark"{return false,nil}
 imageRef:=request.Binding.Source
 if len(presentation.prefix)>0{
  last:=presentation.prefix[len(presentation.prefix)-1]
  imageRef=v2input.Ref{ArtifactID:last.ImageRef.ID,SHA256:last.ImageRef.SHA256}
 }
 pins,err:=delivery.NewWatermarkPinStore(p.DB,lease.Tenant);if err!=nil{return false,err}
 queries:=watermarkObservationQueries{db:p.DB,lease:lease,slot:child.SlotIndex,operation:"derivative.watermark"}
 verified,err:=LoadVerifiedDeliveryWatermarkStage(ctx,queries,pins,p.Reader,p.Bucket,lease,request,source.Scope,imageRef,pending.SourceJCS,p.Registry,p.Limits)
 if err!=nil||verified==nil{return false,err}
 entries,err:=deliveryStageEntries(presentation.entries,verified.Producer());if err!=nil{return false,err}
 node,_,err:=verified.CompileEncodeAfterWatermark(request,source.TargetICCSHA,p.Registry,p.Limits);if err!=nil{return false,err}
 recovery:=watermarkObservationQueries{db:p.DB,lease:lease,slot:child.SlotIndex,operation:node.Operation,paramsSHA:renderDerivativeBytesSHA(node.Params)}
 saved,err:=ReadSavedWatermarkContinuationParams(ctx,recovery,lease,child,node);if err!=nil{return false,err}
 freeze:=func(ref v2input.Ref)(FrozenDeliveryWatermarkEncodeTask,error){return verified.FreezeEncodeAfterWatermark(request,source.TargetICCSHA,p.Registry,p.Limits,ref,entries)}
 documents:=artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:p.DB,Bucket:p.Bucket},Objects:p.Objects}
 publisher:=v2input.ScopedPublisher{Documents:documents,Tenant:lease.Tenant,Run:child.RunID}
 recovered,err:=resumeDeliveryContinuation(ctx,saved,deliveryContinuationActions{
  activated:func(ref v2input.Ref)(bool,error){
   frozen,err:=freeze(ref);if err!=nil{return false,err}
   return (sqlWatermarkEncodeAdmission{db:p.DB,frozen:frozen,lease:lease,request:request}).Activated(ctx)
  },
  open:func()error{
   store:=sqlWatermarkEncodeAdmission{db:p.DB,lease:lease}
   return store.transaction(ctx,func(ctx context.Context,tx pgx.Tx)error{return verified.BeginEncodeContinuationInTransaction(ctx,tx,lease,request,p.Bucket)})
  },
  publish:func()(v2input.Ref,error){return verified.PublishEncodeParamsAfterWatermark(ctx,p.DB,p.Objects,p.Bucket,lease,request,source.TargetICCSHA,p.Registry,p.Limits)},
  admit:func(ref v2input.Ref)(bool,error){
   frozen,err:=freeze(ref);if err!=nil{return false,err}
   return frozen.AdmitEncodeAfterWatermark(ctx,p.DB,publisher,lease,request)
  },
 })
 if err!=nil{return false,err};return !recovered,nil
}
