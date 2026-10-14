package interchange

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ExportSeriesMeasureMaskSource interface{LookupExportSeriesMeasureMask(context.Context,ExportSeriesMeasureOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Ref)(objectstore.Object,error)}
type NativeExportSeriesMeasureMasks struct{Inputs ExportSeriesMeasureInputAuthority;Source ExportSeriesMeasureMaskSource;Objects ImportDocumentObjects;Scratch *ExportArchiveScratch;Probe ExportNativeProbe;RuntimeSHA string}
func(q NativeExportSeriesMeasureMasks)QualifyExportSeriesMeasureInputs(ctx context.Context,origin ExportSeriesMeasureOrigin,stat v2input.Entry,raw []byte,image ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Inputs==nil||q.Source==nil||q.Objects==nil||q.Scratch==nil||q.Probe==nil||!validImportRef(v2input.Ref{ArtifactID:stat.Ref.ArtifactID,SHA256:q.RuntimeSHA}){return v2input.ErrInvalid}
 if _,err:=exportSeriesMeasureInputRefs(origin,raw,image);err!=nil{return err}
 body:=append([]byte(nil),raw...);ownedImage:=ExportClosureNode{Entry:image.Entry,Body:append([]byte(nil),image.Body...),Children:append([]v2input.Entry(nil),image.Children...)}
 cloneImage:=func()ExportClosureNode{return ExportClosureNode{Entry:ownedImage.Entry,Body:append([]byte(nil),ownedImage.Body...),Children:append([]v2input.Entry(nil),ownedImage.Children...)}}
 if err:=q.Inputs.QualifyExportSeriesMeasureInputs(ctx,origin,stat,append([]byte(nil),body...),cloneImage());err!=nil{return err}
 var params struct{Regions []struct{Mask json.RawMessage `json:"mask"`} `json:"regions"`}
 if json.Unmarshal(body,&params)!=nil{return v2input.ErrInvalid}
 for _,region:=range params.Regions{
  mask,err:=review.ParseMaskRef(region.Mask);if err!=nil{return err}
  ref:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}
  read:=func()(objectstore.Object,error){node:=ExportClosureNode{Entry:ownedImage.Entry,Body:append([]byte(nil),ownedImage.Body...),Children:append([]v2input.Entry(nil),ownedImage.Children...)};return q.Source.LookupExportSeriesMeasureMask(ctx,origin,stat,append([]byte(nil),body...),node,ref)}
  object,err:=read();if err!=nil{return err}
  canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return err}
  geometry:=statsMaskGeometry{reference:ref,geometry:ExportMaskGeometry{Width:int(mask.Canvas.Width),Height:int(mask.Canvas.Height),CanvasSHA:canvas}}
  native:=NativeExportMaskQualifier{Objects:q.Objects,Scratch:q.Scratch,Probe:q.Probe,Geometry:geometry,RuntimeSHA:q.RuntimeSHA}
  if err:=native.QualifyExportBinary(ctx,v2input.Entry{Ref:ref,Contract:"MaskPixels"},object);err!=nil{return err}
  current,err:=read();if err!=nil{return err};if current!=object{return v2input.ErrInvalid}
 }
 if err:=q.Inputs.QualifyExportSeriesMeasureInputs(ctx,origin,stat,append([]byte(nil),body...),cloneImage());err!=nil{return err};return ctx.Err()
}
var _ ExportSeriesMeasureInputAuthority=NativeExportSeriesMeasureMasks{}
