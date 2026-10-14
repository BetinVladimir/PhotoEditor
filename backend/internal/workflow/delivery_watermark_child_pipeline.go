package workflow

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// DeliveryResolvedWatermarkChild is supplied only by an authorized resolver:
// exact persisted pin/current ACL, physically checked source arrays and complete
// transitive inventory. Pin value validation alone grants no SQL authority.
type DeliveryResolvedWatermarkChild struct {
 TargetICCSHA string
 ImageJCS []byte
 Entries []v2input.Entry
 Arrays []delivery.WatermarkArrayObject
 Scope delivery.WatermarkPinScope
 Pin delivery.FrozenWatermarkSource
}
type DeliveryWatermarkChildResolver interface {
 ResolveWatermarkChild(context.Context,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent)(DeliveryResolvedWatermarkChild,error)
}

// DeliveryWatermarkChildPipeline composes the first required derivative or
// watermark with immutable Params, metadata and initial admission. Continuation
// observation and assembly must be installed before routing production here.
type DeliveryWatermarkChildPipeline struct {
 DB *pgxpool.Pool
 Objects artifactjson.Objects
 Reader v2input.SessionAnalysisObjectReader
 Bucket string
 Resolver DeliveryWatermarkChildResolver
 Registry Registry
 Limits DeliveryDerivativeLimits
}
func(p DeliveryWatermarkChildPipeline)ProcessChild(ctx context.Context,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent)(DeliveryChildAdmission,error){
 zero:=DeliveryChildAdmission{}
 if err:=ctx.Err();err!=nil{return zero,err}
 if p.DB==nil||p.Objects==nil||p.Reader==nil||p.Resolver==nil||len(p.Bucket)<3||len(p.Bucket)>63||!lease.Valid()||lease.ParentTask!=plan.TaskID()||child.SlotIndex<0||child.SlotIndex>lease.Cursor||child.SlotIndex>=lease.SlotCount{return zero,v2input.ErrInvalid}
 source,err:=p.Resolver.ResolveWatermarkChild(ctx,lease,plan,child);if err!=nil{return zero,err}
 source.ImageJCS=append([]byte(nil),source.ImageJCS...)
 source.Entries=append([]v2input.Entry(nil),source.Entries...)
 source.Arrays=append([]delivery.WatermarkArrayObject(nil),source.Arrays...)
 if source.Scope.Tenant!=lease.Tenant||source.Scope.Parent!=lease.Run{return zero,v2input.ErrInvalid}
 request,_,err:=plan.WatermarkExportRequest(lease.Run,child,source.Scope,source.Pin);if err!=nil{return zero,err}
 next,err:=CompileDeliveryWatermarkPrefix(request,source.Pin,source.Scope,source.ImageJCS,nil,p.Registry,p.Limits);if err!=nil{return zero,err}
 if err=verifyInitialWatermarkSourceArrays(ctx,p.Reader,request.Binding.Source,source.ImageJCS,source.Arrays);err!=nil{return zero,err}
 policy,err:=plan.MetadataPolicy();if err!=nil{return zero,err}
 exportID:=uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1"))
 documents:=artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:p.DB,Bucket:p.Bucket},Objects:p.Objects}
 transport:=v2input.ScopedPublisher{Documents:documents,Tenant:lease.Tenant,Run:child.RunID}
 var task watermarkInitialAdmissionTask
 var config,paramsBody []byte
 if next.Node.Operation=="derivative.watermark"{
  params,err:=PublishInitialWatermarkParams(ctx,p.DB,p.Objects,p.Bucket,lease,request,source.Pin,source.Scope,source.ImageJCS,p.Registry,p.Limits);if err!=nil{return zero,err}
  frozen,err:=FreezeDeliveryWatermarkTask(child.RunID,source.Pin,source.Scope,[]byte(request.ConfigJCS),request.Binding.Source,source.ImageJCS,request.Recipe,request.Preset.Config,params,source.Entries,p.Registry,p.Limits);if err!=nil{return zero,err}
  if policy.MetadataPolicy!="strip"{
   store,err:=delivery.NewMetadataInventoryEntryStore(p.DB,lease.Tenant);if err!=nil{return zero,err}
   frozen,err=frozen.WithMetadataConfig(ctx,store,lease,plan,child);if err!=nil{return zero,err}
  }
  config,err=frozen.initialChildConfig(plan,request,exportID);if err!=nil{return zero,err}
  paramsBody=frozen.task.closure.ParamsJCS;task=frozen
 }else{
  params,err:=PublishInitialWatermarkDerivativeParams(ctx,p.DB,p.Objects,p.Bucket,lease,request,source.Pin,source.Scope,source.ImageJCS,p.Registry,p.Limits);if err!=nil{return zero,err}
  frozen,err:=FreezeDeliveryWatermarkPrefixTask(request,source.Pin,source.Scope,source.ImageJCS,nil,p.Registry,p.Limits,params,source.Entries);if err!=nil{return zero,err}
  if policy.MetadataPolicy!="strip"{
   store,err:=delivery.NewMetadataInventoryEntryStore(p.DB,lease.Tenant);if err!=nil{return zero,err}
   frozen,err=frozen.WithMetadataConfig(ctx,store,lease,plan,child);if err!=nil{return zero,err}
  }
  config,err=frozen.initialChildConfig(plan,request,exportID);if err!=nil{return zero,err}
  paramsBody=frozen.task.closure.ParamsJCS;task=watermarkInitialDerivativeAdapter{frozen}
 }
 draft,err:=task.DraftTask();if err!=nil{return zero,err}
 estimate,err:=ParseResourceEstimate(draft.ExecutionProfile);if err!=nil{return zero,err}
 overhead:=int64(len(config)+len(paramsBody))
 if estimate.OutputBytes<1||estimate.OutputBytes>9007199254740991-overhead{return zero,v2input.ErrInvalid}
 recovered,err:=admitInitialWatermark(ctx,p.DB,transport,task,lease,plan,child,exportID,estimate.OutputBytes+overhead);if err!=nil{return zero,err}
 return DeliveryChildAdmission{RunID:child.RunID,ExportID:exportID,NextCursor:child.SlotIndex+1,Recovered:recovered},nil
}
