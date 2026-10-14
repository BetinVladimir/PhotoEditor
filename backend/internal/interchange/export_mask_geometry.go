package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Resolve only MaskRef JSON nodes, never their binary children: this avoids
// recursive physical qualification. The resolver must use the scoped catalogue
// and installed source/model/transform semantic validator.
type ExportTypedMaskGeometrySource struct{
 resolver ExportClosureNodeResolver
 masks []v2input.Entry
 authority ExportMaskArrayAuthority
}

func NewExportTypedMaskGeometrySource(resolver ExportClosureNodeResolver,masks []v2input.Entry)(*ExportTypedMaskGeometrySource,error){
 if resolver==nil||len(masks)<1||len(masks)>v2input.MaxEntries{return nil,v2input.ErrInvalid}
 seen:=map[uuid.UUID]bool{}
 owned:=append([]v2input.Entry(nil),masks...)
 for _,entry:=range owned{if entry.Contract!="MaskRef"||!validImportRef(entry.Ref)||seen[entry.Ref.ArtifactID]{return nil,v2input.ErrInvalid};seen[entry.Ref.ArtifactID]=true}
 return &ExportTypedMaskGeometrySource{resolver:resolver,masks:owned},nil
}

func(s *ExportTypedMaskGeometrySource)ResolveExportMaskGeometry(ctx context.Context,ref v2input.Ref)(ExportMaskGeometry,error){
 var zero ExportMaskGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 if s==nil||s.resolver==nil||!validImportRef(ref){return zero,v2input.ErrInvalid}
 found:=false;var result ExportMaskGeometry
 for _,entry:=range s.masks{
  if err:=ctx.Err();err!=nil{return zero,err}
  node,err:=s.resolver.ResolveExportNode(ctx,entry);if err!=nil{return zero,err}
  if node.Entry!=entry||rawSHA(node.Body)!=entry.Ref.SHA256{return zero,v2input.ErrInvalid}
  mask,err:=review.ParseMaskRef(node.Body);if err!=nil{return zero,err}
  array:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}
  if array!=ref{continue}
  // The schema-derived child list must independently bind the binary leaf.
  childFound:=false
  var arrayEntry v2input.Entry
  for _,child:=range node.Children{if child.Ref.ArtifactID==ref.ArtifactID{
   if child.Ref!=ref||!exportMaskBinaryContract(child.Contract)||(childFound&&arrayEntry!=child){return zero,v2input.ErrInvalid};childFound=true;arrayEntry=child
  }}
  if !childFound{return zero,v2input.ErrInvalid}
  if s.authority!=nil{if err:=s.authority.QualifyExportMaskArray(ctx,entry,append([]byte(nil),node.Body...),arrayEntry);err!=nil{return zero,err}}
  canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return zero,err}
  geometry:=ExportMaskGeometry{Width:int(mask.Canvas.Width),Height:int(mask.Canvas.Height),CanvasSHA:canvas}
  if !validExportMaskGeometry(geometry,ref){return zero,v2input.ErrInvalid}
  if found&&result!=geometry{return zero,v2input.ErrInvalid}
  result=geometry;found=true
 }
 if !found{return zero,v2input.ErrInvalid};return result,ctx.Err()
}
var _ ExportMaskGeometrySource=(*ExportTypedMaskGeometrySource)(nil)
