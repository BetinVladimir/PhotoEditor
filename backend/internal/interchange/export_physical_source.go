package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// JSON reader is deliberately separate from this adapter to avoid native
// geometry discovery recursively invoking itself.
type ExportPhysicalSourceResolver struct {
 JSON ExportClosureNodeResolver
 Catalog ExportDocumentCatalog
 Objects ImportDocumentObjects
 Working ExportWorkingArrayAuthority
 Warps ExportDenseWarpOccurrenceAuthority
 Scratch *ExportArchiveScratch
 Probe ExportNativeProbe
 RuntimeSHA string
}
func(r ExportPhysicalSourceResolver)ResolveExportNode(ctx context.Context,e v2input.Entry)(ExportClosureNode,error){
 var zero ExportClosureNode
 if err:=ctx.Err();err!=nil{return zero,err}
 if e.Contract!="WorkingImage"||r.JSON==nil||r.Catalog==nil||r.Objects==nil||r.Working==nil||r.Warps==nil||r.Scratch==nil||r.Probe==nil{return zero,v2input.ErrInvalid}
 node,err:=r.JSON.ResolveExportNode(ctx,e);if err!=nil{return zero,err}
 doc,err:=ownExportCompositionDocument(ctx,node,e);if err!=nil{return zero,err}
 image,err:=review.ParseWorkingImage(doc.Body);if err!=nil{return zero,err}
 node=ExportClosureNode{Entry:e,Body:append([]byte(nil),doc.Body...),Children:append([]v2input.Entry(nil),node.Children...)}
 if err:=validateExportPhysicalSourceChildren(image,node.Children);err!=nil{return zero,err}
 geometry,err:=NewExportTypedArrayGeometrySource(r.JSON,[]v2input.Entry{e});if err!=nil{return zero,err}
 geometry.workingAuthority=r.Working;geometry.warpAuthority=r.Warps
 native:=NativeExportArrayQualifier{Objects:r.Objects,Scratch:r.Scratch,Probe:r.Probe,Geometry:geometry,RuntimeSHA:r.RuntimeSHA}
 pixels:=false;alpha:=image.Alpha==nil
 for _,child:=range node.Children{
  switch child.Contract{case "WorkingPixels","WorkingAlpha","DenseWarpField":default:return zero,v2input.ErrInvalid}
  object,err:=r.Catalog.LookupExportDocument(ctx,child);if err!=nil{return zero,err}
  if err:=native.QualifyExportBinary(ctx,child,object);err!=nil{return zero,err}
  final,err:=r.Catalog.LookupExportDocument(ctx,child);if err!=nil{return zero,err};if final!=object{return zero,v2input.ErrInvalid}
  if child.Contract=="WorkingPixels"{pixels=true};if child.Contract=="WorkingAlpha"{alpha=true}
 }
 if !pixels||!alpha{return zero,v2input.ErrInvalid}
 if err:=qualifyExportResolvedSourceWarps(ctx,node,r.Warps);err!=nil{return zero,err}
 final,err:=r.JSON.ResolveExportNode(ctx,e);if err!=nil{return zero,err}
 finalDoc,err:=ownExportCompositionDocument(ctx,final,e);if err!=nil{return zero,err};if !bytes.Equal(doc.Body,finalDoc.Body){return zero,v2input.ErrInvalid}
 if err:=validateExportPhysicalSourceChildren(image,final.Children);err!=nil{return zero,err}
 // The last JSON read may observe revoked scope; do not return an earlier
 // array/warp permission after that IO boundary.
 for _,child:=range node.Children{if child.Contract=="WorkingPixels"||child.Contract=="WorkingAlpha"{if err:=r.Working.QualifyExportWorkingArray(ctx,e,child);err!=nil{return zero,err}}}
 if err:=qualifyExportResolvedSourceWarps(ctx,ExportClosureNode{Entry:e,Body:append([]byte(nil),doc.Body...),Children:append([]v2input.Entry(nil),node.Children...)},r.Warps);err!=nil{return zero,err}
 return ExportClosureNode{Entry:e,Body:append([]byte(nil),doc.Body...),Children:append([]v2input.Entry(nil),node.Children...)},ctx.Err()
}

// Require the complete closed array inventory before opening any binary stream.
func validateExportPhysicalSourceChildren(image review.WorkingImage,children []v2input.Entry)error{
 expected:=map[uuid.UUID]v2input.Entry{}
 add:=func(ref review.ArtifactRef,contract string)error{
  entry:=v2input.Entry{Contract:contract,Ref:v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256}}
  if !validImportRef(entry.Ref){return v2input.ErrInvalid}
  if previous,ok:=expected[ref.ID];ok&&previous!=entry{return v2input.ErrInvalid}
  expected[ref.ID]=entry;return nil
 }
 if err:=add(image.Pixels,"WorkingPixels");err!=nil{return err}
 if image.Alpha!=nil{if err:=add(*image.Alpha,"WorkingAlpha");err!=nil{return err}}
 var chain struct{Steps []struct{Inverse *review.ArtifactRef `json:"inverse_field"`} `json:"steps"`}
 if json.Unmarshal(image.TransformChain,&chain)!=nil{return v2input.ErrInvalid}
 for _,step:=range chain.Steps{if step.Inverse!=nil{if err:=add(*step.Inverse,"DenseWarpField");err!=nil{return err}}}
 if len(children)!=len(expected){return v2input.ErrInvalid}
 seen:=map[uuid.UUID]bool{}
 for _,child:=range children{if seen[child.Ref.ArtifactID]||expected[child.Ref.ArtifactID]!=child{return v2input.ErrInvalid};seen[child.Ref.ArtifactID]=true}
 return nil
}
