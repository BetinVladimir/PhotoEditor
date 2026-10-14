package interchange

import (
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ExportBodyPersonSource interface{LookupExportBodyPerson(context.Context,v2input.Entry,[]byte,ExportClosureNode)(objectstore.Object,error)}
type QualifiedExportBodyPersonSource struct{Origin ExportBodyPersonOriginSource;Documents ExportStatsPersonDocumentReader;Policy ExportStatsPersonPolicyAuthority}
func(s QualifiedExportBodyPersonSource)LookupExportBodyPerson(ctx context.Context,parent v2input.Entry,raw []byte,image ExportClosureNode)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Origin==nil||s.Policy==nil||s.Documents.Catalog==nil||s.Documents.Objects==nil{return zero,v2input.ErrInvalid}
 projection,err:=exportBodyPersonProjection(parent,raw,image);if err!=nil{return zero,err}
 body:=append([]byte(nil),raw...);imageBody:=append([]byte(nil),image.Body...)
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:image.Entry,Body:append([]byte(nil),imageBody...),Children:append([]v2input.Entry(nil),image.Children...)}}
 origin,err:=s.Origin.LookupExportBodyPersonOrigin(ctx,parent,append([]byte(nil),body...),clone());if err!=nil{return zero,err}
 policy,err:=s.Policy.ResolveExportStatsPersonPolicy(ctx,origin,image.Entry.Ref);if err!=nil{return zero,err}
 canvas,err:=canonical.Hash(projection.Person.Canvas);if err!=nil{return zero,err}
 if policy.Binding.Source!=image.Entry.Ref||policy.Binding.AssetVersionID==([16]byte{})||policy.Binding.CanvasHash!=canvas||!validImportRef(v2input.Ref{ArtifactID:image.Entry.Ref.ArtifactID,SHA256:policy.QualificationSHA})||projection.Person.ModelLockID==nil||*projection.Person.ModelLockID!=policy.ModelLock{return zero,v2input.ErrInvalid}
 docs,err:=s.Documents.ReadExportStatsPersonDocuments(ctx,origin);if err!=nil{return zero,err}
 projected,err:=canonical.Marshal(projection);if err!=nil{return zero,err}
 if err:=VerifyExportStatsPersonDocuments(ctx,origin,docs,projected,imageBody,policy.Binding,policy.ModelLock,policy.Backend);err!=nil{return zero,err}
 final,err:=s.Origin.LookupExportBodyPersonOrigin(ctx,parent,append([]byte(nil),body...),clone());if err!=nil{return zero,err};if final!=origin{return zero,v2input.ErrInvalid}
 current,err:=s.Policy.ResolveExportStatsPersonPolicy(ctx,final,image.Entry.Ref);if err!=nil{return zero,err};if current!=policy{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err};return origin.Mask,nil
}
var _ ExportBodyPersonSource=QualifiedExportBodyPersonSource{}

type ExportBodyPersonAuthority interface{QualifyExportBodyPerson(context.Context,v2input.Entry,[]byte,ExportClosureNode)error}
type NativeExportBodyPersonQualifier struct{Source ExportBodyPersonSource;Objects ImportDocumentObjects;Scratch *ExportArchiveScratch;Probe ExportNativeProbe;RuntimeSHA string}
func(q NativeExportBodyPersonQualifier)QualifyExportBodyPerson(ctx context.Context,parent v2input.Entry,raw []byte,image ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Source==nil||q.Objects==nil||q.Scratch==nil||q.Probe==nil{return v2input.ErrInvalid}
 projection,err:=exportBodyPersonProjection(parent,raw,image);if err!=nil{return err}
 body:=append([]byte(nil),raw...);imageBody:=append([]byte(nil),image.Body...)
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:image.Entry,Body:append([]byte(nil),imageBody...),Children:append([]v2input.Entry(nil),image.Children...)}}
 before,err:=q.Source.LookupExportBodyPerson(ctx,parent,append([]byte(nil),body...),clone());if err!=nil{return err}
 canvas,err:=canonical.Hash(projection.Person.Canvas);if err!=nil{return err}
 ref:=v2input.Ref{ArtifactID:projection.Person.Artifact.ID,SHA256:projection.Person.Artifact.SHA256}
 geometry:=statsMaskGeometry{reference:ref,geometry:ExportMaskGeometry{Width:int(projection.Person.Canvas.Width),Height:int(projection.Person.Canvas.Height),CanvasSHA:canvas}}
 native:=NativeExportMaskQualifier{Objects:q.Objects,Scratch:q.Scratch,Probe:q.Probe,Geometry:geometry,RuntimeSHA:q.RuntimeSHA}
 if err:=native.QualifyExportBinary(ctx,v2input.Entry{Ref:ref,Contract:"MaskPixels"},before);err!=nil{return err}
 after,err:=q.Source.LookupExportBodyPerson(ctx,parent,append([]byte(nil),body...),clone());if err!=nil{return err};if before!=after{return v2input.ErrInvalid};return ctx.Err()
}
var _ ExportBodyPersonAuthority=NativeExportBodyPersonQualifier{}
