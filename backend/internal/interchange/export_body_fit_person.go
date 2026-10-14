package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Implementations must retain the original pair.fit.body consumer identity and
// exact side edge; projecting a synthetic geometry.body consumer is forbidden.
type ExportBodyFitPersonOriginSource interface {
 LookupExportBodyFitPersonOrigin(context.Context,v2input.Entry,[]byte,ExportClosureNode,string,ExportClosureNode)(ExportStatsPersonOrigin,error)
}
type NativeExportBodyFitPairedPersons struct {
 Inventory ExportBodyFitInventoryAuthority
 Origin ExportBodyFitPersonOriginSource
 Images ExportClosureNodeResolver
 Warps ExportDenseWarpOccurrenceAuthority
 Documents ExportStatsPersonDocumentReader
 Policy ExportStatsPersonPolicyAuthority
 Objects ImportDocumentObjects
 Scratch *ExportArchiveScratch
 Probe ExportNativeProbe
 RuntimeSHA string
}
func(q NativeExportBodyFitPairedPersons)QualifyExportBodyFitPairedPersons(ctx context.Context,parent v2input.Entry,raw []byte,fit ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Inventory==nil||q.Origin==nil||q.Images==nil||q.Warps==nil||q.Documents.Catalog==nil||q.Documents.Objects==nil||q.Policy==nil||q.Objects==nil||q.Scratch==nil||q.Probe==nil||parent.Contract!="Params"||!validImportRef(parent.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256{return v2input.ErrInvalid}
 raw=append([]byte(nil),raw...);fit=ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}
 inventory,err:=exportBodyFitInputInventory(fit);if err!=nil{return err}
 cloneFit:=func()ExportClosureNode{return ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}}
 check:=func()error{return q.Inventory.QualifyExportBodyFitInputInventory(ctx,parent,append([]byte(nil),raw...),cloneFit())}
 if err:=check();err!=nil{return err}
 var fields struct{Params map[string]json.RawMessage `json:"params"`}
 if json.Unmarshal(fit.Body,&fields)!=nil{return v2input.ErrInvalid}
 for _,side:=range []struct{name string;ref v2input.Ref}{{"before",inventory.Before},{"after",inventory.After}}{
  mask,err:=review.ParseMaskRef(fields.Params[side.name+"_person"]);if err!=nil{return err}
  entry:=v2input.Entry{Contract:"WorkingImage",Ref:side.ref}
  node,err:=q.Images.ResolveExportNode(ctx,entry);if err!=nil{return err}
  doc,err:=ownExportCompositionDocument(ctx,node,entry);if err!=nil{return err}
  image,err:=review.ParseWorkingImage(doc.Body);if err!=nil{return err}
  projection:=review.StatsParams{Image:review.ArtifactRef{ID:side.ref.ArtifactID,SHA256:side.ref.SHA256},Person:mask}
  if err:=projection.ValidateSource(image,projection.Image);err!=nil{return err}
  if image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Canvas.Width*image.Canvas.Height>40000000{return v2input.ErrInvalid}
  if err:=qualifyExportResolvedSourceWarps(ctx,node,q.Warps);err!=nil{return err}
  cloneImage:=func()ExportClosureNode{return ExportClosureNode{Entry:entry,Body:append([]byte(nil),doc.Body...),Children:append([]v2input.Entry(nil),node.Children...)}}
  origin,err:=q.Origin.LookupExportBodyFitPersonOrigin(ctx,parent,append([]byte(nil),raw...),cloneFit(),side.name,cloneImage());if err!=nil{return err}
  policy,err:=q.Policy.ResolveExportStatsPersonPolicy(ctx,origin,side.ref);if err!=nil{return err}
  canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return err}
  if policy.Binding.Source!=side.ref||policy.Binding.AssetVersionID==([16]byte{})||policy.Binding.CanvasHash!=canvas||mask.ModelLockID==nil||*mask.ModelLockID!=policy.ModelLock||!validImportRef(v2input.Ref{ArtifactID:side.ref.ArtifactID,SHA256:policy.QualificationSHA}){return v2input.ErrInvalid}
  docs,err:=q.Documents.ReadExportStatsPersonDocuments(ctx,origin);if err!=nil{return err}
  projected,err:=canonical.Marshal(projection);if err!=nil{return err}
  if err:=VerifyExportStatsPersonDocuments(ctx,origin,docs,projected,doc.Body,policy.Binding,policy.ModelLock,policy.Backend);err!=nil{return err}
  ref:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}
  native:=NativeExportMaskQualifier{Objects:q.Objects,Scratch:q.Scratch,Probe:q.Probe,RuntimeSHA:q.RuntimeSHA,Geometry:statsMaskGeometry{reference:ref,geometry:ExportMaskGeometry{Width:int(mask.Canvas.Width),Height:int(mask.Canvas.Height),CanvasSHA:canvas}}}
  if err:=native.QualifyExportBinary(ctx,v2input.Entry{Contract:"MaskPixels",Ref:ref},origin.Mask);err!=nil{return err}
  final,err:=q.Origin.LookupExportBodyFitPersonOrigin(ctx,parent,append([]byte(nil),raw...),cloneFit(),side.name,cloneImage());if err!=nil{return err};if final!=origin{return v2input.ErrInvalid}
  current,err:=q.Policy.ResolveExportStatsPersonPolicy(ctx,final,side.ref);if err!=nil{return err};if current!=policy{return v2input.ErrInvalid}
  finalNode,err:=q.Images.ResolveExportNode(ctx,entry);if err!=nil{return err}
  finalDoc,err:=ownExportCompositionDocument(ctx,finalNode,entry);if err!=nil{return err};if !bytes.Equal(doc.Body,finalDoc.Body){return v2input.ErrInvalid}
  if err:=qualifyExportResolvedSourceWarps(ctx,finalNode,q.Warps);err!=nil{return err}
 }
 if err:=check();err!=nil{return err};return ctx.Err()
}
var _ ExportBodyFitPairedPersonAuthority=NativeExportBodyFitPairedPersons{}
