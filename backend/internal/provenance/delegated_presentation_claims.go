package provenance

import "context"

// DelegatedPresentationTaskResolver supports only operations whose immutable
// claims have a delegated proof path. Unknown operations fail closed.
type DelegatedPresentationTaskResolver struct{
 Documents DelegatedTaskDocuments
 Producers DelegatedImageProducer
 Originals DelegatedOriginalSource
 WorkICCSHA string
 Render DelegatedTaskClaims
 Negative DelegatedTaskClaims
}

func(s DelegatedPresentationTaskResolver)ResolveDelegatedTask(ctx context.Context,
 ready ReadySigningExport,task CanonicalSigningTask)(ResolvedSigningTask,error){
 if err:=ctx.Err();err!=nil{return ResolvedSigningTask{},err}
 if task.Version!="1"||s.Documents==nil{
  return ResolvedSigningTask{},ErrInvalid
 }
 switch task.Operation{
 case "asset.decode":
  return (DelegatedDecodeSigningTaskResolver{Documents:s.Documents,
   Originals:s.Originals,WorkICCSHA:s.WorkICCSHA}).ResolveDelegatedTask(ctx,ready,task)
 case "export.encode":
  return (DelegatedEncodeSigningTaskResolver{Documents:s.Documents,
   Producers:s.Producers}).ResolveDelegatedTask(ctx,ready,task)
 case "metadata.write":
  return (DelegatedMetadataSigningTaskResolver{Documents:s.Documents,
   Producers:s.Producers}).ResolveDelegatedTask(ctx,ready,task)
 case "derivative.resize":
  return (DelegatedResizeSigningTaskResolver{Documents:s.Documents,
   Producers:s.Producers}).ResolveDelegatedTask(ctx,ready,task)
 case "derivative.sharpen":
  return (DelegatedSharpenSigningTaskResolver{Documents:s.Documents,
   Producers:s.Producers}).ResolveDelegatedTask(ctx,ready,task)
 case "derivative.watermark":
  return (DelegatedWatermarkSigningTaskResolver{Documents:s.Documents,
   Producers:s.Producers}).ResolveDelegatedTask(ctx,ready,task)
 case "render.compose":
  if s.Render==nil{return ResolvedSigningTask{},ErrInvalid}
  return s.Render.ResolveDelegatedTask(ctx,ready,task)
 case "optical.negative":
  if s.Negative==nil{return ResolvedSigningTask{},ErrInvalid}
  return s.Negative.ResolveDelegatedTask(ctx,ready,task)
 default:
  return ResolvedSigningTask{},ErrInvalid
 }
}

var _ DelegatedTaskClaims = DelegatedPresentationTaskResolver{}
