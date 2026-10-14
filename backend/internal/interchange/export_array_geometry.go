package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Parents are validated JSON nodes, read afresh through the scoped catalogue.
// Physical leaf qualification must not be called recursively by this resolver.
type ExportTypedArrayGeometrySource struct{resolver ExportClosureNodeResolver;parents []v2input.Entry;workingAuthority ExportWorkingArrayAuthority;warpAuthority ExportDenseWarpOccurrenceAuthority;frameAuthority ExportWorkingArrayOccurrenceAuthority}
func NewExportTypedArrayGeometrySource(resolver ExportClosureNodeResolver,parents []v2input.Entry)(*ExportTypedArrayGeometrySource,error){
 if resolver==nil||len(parents)<1||len(parents)>v2input.MaxEntries{return nil,v2input.ErrInvalid}
 owned:=append([]v2input.Entry(nil),parents...);seen:=map[uuid.UUID]bool{}
 for _,entry:=range owned{if (entry.Contract!="WorkingImage"&&entry.Contract!="MaskRef")||!validImportRef(entry.Ref)||seen[entry.Ref.ArtifactID]{return nil,v2input.ErrInvalid};seen[entry.Ref.ArtifactID]=true}
 return &ExportTypedArrayGeometrySource{resolver:resolver,parents:owned},nil
}

func(s *ExportTypedArrayGeometrySource)ResolveExportArrayGeometry(ctx context.Context,entry v2input.Entry)(ExportArrayGeometry,error){
 var zero ExportArrayGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 if s==nil||s.resolver==nil||!exportArrayBinaryContract(entry.Contract)||!validImportRef(entry.Ref){return zero,v2input.ErrInvalid}
 found:=false;var result ExportArrayGeometry
 accept:=func(parent v2input.Entry,node ExportClosureNode,ref review.ArtifactRef,canvas review.Canvas,dtype string)error{
  if ref.ID!=entry.Ref.ArtifactID||ref.SHA256!=entry.Ref.SHA256{return nil}
  childFound:=false
  for _,child:=range node.Children{if child.Ref.ArtifactID==entry.Ref.ArtifactID{
   if child!=entry{return v2input.ErrInvalid};childFound=true
  }}
  if !childFound{return v2input.ErrInvalid}
  if (entry.Contract=="WorkingPixels"||entry.Contract=="WorkingAlpha")&&s.workingAuthority!=nil{if err:=s.workingAuthority.QualifyExportWorkingArray(ctx,parent,entry);err!=nil{return err}}
  if (entry.Contract=="WorkingPixels"||entry.Contract=="WorkingAlpha")&&s.frameAuthority!=nil{if err:=s.frameAuthority.QualifyExportWorkingArrayOccurrence(ctx,parent,append([]byte(nil),node.Body...),entry);err!=nil{return err}}
  digest,err:=canonical.Hash(canvas);if err!=nil{return err}
  geometry:=ExportArrayGeometry{Width:int(canvas.Width),Height:int(canvas.Height),CanvasSHA:digest,DType:dtype}
  if _,err:=exportArrayMaximumBytes(entry,geometry);err!=nil{return err}
  if found&&result!=geometry{return v2input.ErrInvalid};result=geometry;found=true;return nil
 }
 for _,parent:=range s.parents{
  if err:=ctx.Err();err!=nil{return zero,err}
  node,err:=s.resolver.ResolveExportNode(ctx,parent);if err!=nil{return zero,err}
  if node.Entry!=parent||rawSHA(node.Body)!=parent.Ref.SHA256{return zero,v2input.ErrInvalid}
  var chain json.RawMessage
  switch parent.Contract{
  case "WorkingImage":
   image,err:=review.ParseWorkingImage(node.Body);if err!=nil{return zero,err};chain=image.TransformChain
   if entry.Contract=="WorkingPixels"{if err:=accept(parent,node,image.Pixels,image.Canvas,image.DType);err!=nil{return zero,err}}
   if entry.Contract=="WorkingAlpha"&&image.Alpha!=nil{if err:=accept(parent,node,*image.Alpha,image.Canvas,"float32");err!=nil{return zero,err}}
  case "MaskRef":
   mask,err:=review.ParseMaskRef(node.Body);if err!=nil{return zero,err};chain=mask.TransformChain
  }
  if entry.Contract=="DenseWarpField"{
   // ParseWorkingImage/ParseMaskRef already validate chain hash, continuity,
   // source binding, inverse-field kind and target canvas before this extraction.
   var typed struct{Steps []struct{To review.Canvas `json:"to_canvas"`;Inverse *review.ArtifactRef `json:"inverse_field"`} `json:"steps"`}
   if err:=json.Unmarshal(chain,&typed);err!=nil{return zero,err}
   for index,step:=range typed.Steps{if step.Inverse!=nil{
    if step.Inverse.ID==entry.Ref.ArtifactID&&step.Inverse.SHA256==entry.Ref.SHA256&&s.warpAuthority!=nil{
     if err:=s.warpAuthority.QualifyExportDenseWarpOccurrence(ctx,parent,append([]byte(nil),node.Body...),index,entry);err!=nil{return zero,err}
    }
    if err:=accept(parent,node,*step.Inverse,step.To,"float32");err!=nil{return zero,err}
   }}
  }
 }
 if !found{return zero,v2input.ErrInvalid};return result,ctx.Err()
}
var _ ExportArrayGeometrySource=(*ExportTypedArrayGeometrySource)(nil)
