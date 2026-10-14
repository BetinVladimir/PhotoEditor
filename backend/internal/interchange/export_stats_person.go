package interchange
import (
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Must prove original StatsParams input membership and the current completed
// person-mask producer/model/qualification before returning immutable bytes.
type ExportStatsPersonSource interface {
 LookupExportStatsPerson(context.Context,v2input.Entry,v2input.Ref,[]byte,review.WorkingImage)(objectstore.Object,error)
}
type NativeExportStatsPersonQualifier struct {
 Source ExportStatsPersonSource
 Objects ImportDocumentObjects
 Scratch *ExportArchiveScratch
 Probe ExportNativeProbe
 RuntimeSHA string
}
type statsMaskGeometry struct{geometry ExportMaskGeometry;reference v2input.Ref}
func(s statsMaskGeometry)ResolveExportMaskGeometry(ctx context.Context,ref v2input.Ref)(ExportMaskGeometry,error){
 if err:=ctx.Err();err!=nil{return ExportMaskGeometry{},err};if ref!=s.reference{return ExportMaskGeometry{},v2input.ErrInvalid};return s.geometry,nil
}
func(q NativeExportStatsPersonQualifier)QualifyExportStatsPerson(ctx context.Context,entry v2input.Entry,paramsRef v2input.Ref,raw []byte,_ review.StatsParams,image review.WorkingImage)error {
 if err:=ctx.Err();err!=nil{return err}
 if q.Source==nil||q.Objects==nil||q.Scratch==nil||q.Probe==nil||entry.Contract!="Stats20"||!validImportRef(entry.Ref)||!validImportRef(paramsRef)||!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:q.RuntimeSHA})||rawSHA(raw)!=paramsRef.SHA256{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 params,err:=review.ParseStatsParams(body);if err!=nil{return err}
 imageRaw,err:=canonical.Marshal(image);if err!=nil{return err}
 ownedImage,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return err}
 if rawSHA(imageRaw)!=params.Image.SHA256{return v2input.ErrInvalid}
 if err:=params.ValidateSource(ownedImage,params.Image);err!=nil{return err}
 read:=func()(objectstore.Object,error){
  owned,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return objectstore.Object{},err}
  return q.Source.LookupExportStatsPerson(ctx,entry,paramsRef,append([]byte(nil),body...),owned)
 }
 object,err:=read();if err!=nil{return err}
 canvas,err:=canonical.Hash(params.Person.Canvas);if err!=nil{return err}
 maskRef:=v2input.Ref{ArtifactID:params.Person.Artifact.ID,SHA256:params.Person.Artifact.SHA256}
 geometry:=statsMaskGeometry{reference:maskRef,geometry:ExportMaskGeometry{Width:int(params.Person.Canvas.Width),Height:int(params.Person.Canvas.Height),CanvasSHA:canvas}}
 qualifier:=NativeExportMaskQualifier{Objects:q.Objects,Scratch:q.Scratch,Probe:q.Probe,Geometry:geometry,RuntimeSHA:q.RuntimeSHA}
 if err:=qualifier.QualifyExportBinary(ctx,v2input.Entry{Ref:maskRef,Contract:"MaskPixels"},object);err!=nil{return err}
 current,err:=read();if err!=nil{return err};if current!=object{return v2input.ErrInvalid}
 return ctx.Err()
}
var _ ExportStatsPersonAuthority=NativeExportStatsPersonQualifier{}
