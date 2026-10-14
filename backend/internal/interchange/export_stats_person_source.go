package interchange

import (
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ExportStatsPersonOriginSource interface {
 LookupExportStatsPersonOrigin(context.Context,v2input.Entry,v2input.Ref,[]byte,review.WorkingImage)(ExportStatsPersonOrigin,error)
}
// QualificationSHA identifies exact verified imported qualification evidence.
// The authority must also validate its current enabled operator policy, model
// assets, license, producer admission profile and accepted scoped source.
type ExportStatsPersonPolicyEvidence struct {
 Binding v2input.SessionVersionBinding
 ModelLock string
 Backend string
 QualificationSHA string
}
type ExportStatsPersonPolicyAuthority interface {
 ResolveExportStatsPersonPolicy(context.Context,ExportStatsPersonOrigin,v2input.Ref)(ExportStatsPersonPolicyEvidence,error)
}
type QualifiedExportStatsPersonSource struct {
 Origin ExportStatsPersonOriginSource
 Documents ExportStatsPersonDocumentReader
 Policy ExportStatsPersonPolicyAuthority
}
func(s QualifiedExportStatsPersonSource)LookupExportStatsPerson(ctx context.Context,entry v2input.Entry,paramsRef v2input.Ref,raw []byte,image review.WorkingImage)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Origin==nil||s.Policy==nil||s.Documents.Catalog==nil||s.Documents.Objects==nil||entry.Contract!="Stats20"||!validImportRef(entry.Ref)||!validImportRef(paramsRef)||rawSHA(raw)!=paramsRef.SHA256{return zero,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 stats,err:=review.ParseStatsParams(body);if err!=nil{return zero,err}
 imageRaw,err:=canonical.Marshal(image);if err!=nil{return zero,err}
 if rawSHA(imageRaw)!=stats.Image.SHA256{return zero,v2input.ErrInvalid}
 source:=v2input.Ref{ArtifactID:stats.Image.ID,SHA256:stats.Image.SHA256}
 readOrigin:=func()(ExportStatsPersonOrigin,error){
  owned,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return ExportStatsPersonOrigin{},err}
  if err:=stats.ValidateSource(owned,stats.Image);err!=nil{return ExportStatsPersonOrigin{},err}
  return s.Origin.LookupExportStatsPersonOrigin(ctx,entry,paramsRef,append([]byte(nil),body...),owned)
 }
 origin,err:=readOrigin();if err!=nil{return zero,err}
 policy,err:=s.Policy.ResolveExportStatsPersonPolicy(ctx,origin,source);if err!=nil{return zero,err}
 if policy.Binding.Source!=source||!validImportRef(v2input.Ref{ArtifactID:source.ArtifactID,SHA256:policy.QualificationSHA}){return zero,v2input.ErrInvalid}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return zero,err}
 if policy.Binding.CanvasHash!=canvas||policy.Binding.AssetVersionID==([16]byte{})||!validImportRef(v2input.Ref{ArtifactID:source.ArtifactID,SHA256:policy.ModelLock})||stats.Person.ModelLockID==nil||*stats.Person.ModelLockID!=policy.ModelLock||(policy.Backend!="cpu"&&policy.Backend!="cuda"&&policy.Backend!="mps"){return zero,v2input.ErrInvalid}
 documents,err:=s.Documents.ReadExportStatsPersonDocuments(ctx,origin);if err!=nil{return zero,err}
 if err:=VerifyExportStatsPersonDocuments(ctx,origin,documents,body,imageRaw,policy.Binding,policy.ModelLock,policy.Backend);err!=nil{return zero,err}
 current,err:=readOrigin();if err!=nil{return zero,err};if current!=origin{return zero,v2input.ErrInvalid}
 currentPolicy,err:=s.Policy.ResolveExportStatsPersonPolicy(ctx,current,source);if err!=nil{return zero,err}
 if currentPolicy!=policy{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err};return origin.Mask,nil
}
var _ ExportStatsPersonOriginSource=SQLExportDocumentCatalog{}
var _ ExportStatsPersonSource=QualifiedExportStatsPersonSource{}
