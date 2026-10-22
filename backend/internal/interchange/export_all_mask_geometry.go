package interchange
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
// Dispatch by proved typed parent edges, never by trying a second source after
// an authority refusal. Shared leaves require every matching parent authority.
type ExportAllMaskGeometrySource struct {
 Warps ExportDenseWarpOccurrenceAuthority
 ParamsAuthority ExportParamsMaskAuthority
 ReportAuthority ExportReportMaskAuthority
 Reader ExportClosureNodeResolver
 Parents []v2input.Entry
 MaskAuthority ExportMaskArrayAuthority
 ResolvedOrigin ExportResolvedOriginSource
}
func(s ExportAllMaskGeometrySource)ResolveExportMaskGeometry(ctx context.Context,ref v2input.Ref)(ExportMaskGeometry,error){
 var zero,result ExportMaskGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Reader==nil||!validImportRef(ref)||len(s.Parents)==0||len(s.Parents)>v2input.MaxEntries{return zero,v2input.ErrInvalid}
 found:=false;seen:=map[uuid.UUID]bool{}
 for _,parent:=range s.Parents{
  if !validImportRef(parent.Ref)||seen[parent.Ref.ArtifactID]{return zero,v2input.ErrInvalid};seen[parent.Ref.ArtifactID]=true
  if parent.Contract!="MaskRef"&&parent.Contract!="ResolvedStyle"&&parent.Contract!="Override"&&parent.Contract!="Params"&&parent.Contract!="DefectProposeReport"{return zero,v2input.ErrInvalid}
  node,err:=s.Reader.ResolveExportNode(ctx,parent);if err!=nil{return zero,err}
  if node.Entry!=parent||rawSHA(node.Body)!=parent.Ref.SHA256{return zero,v2input.ErrInvalid}
  matches:=false
  for _,child:=range node.Children{if child.Ref.ArtifactID==ref.ArtifactID{
   if child.Ref!=ref||!exportMaskBinaryContract(child.Contract){return zero,v2input.ErrInvalid};matches=true
  }}
  if !matches{continue}
  var geometry ExportMaskGeometry
  if parent.Contract=="MaskRef"{
   if s.MaskAuthority==nil{return zero,v2input.ErrInvalid}
   source,createErr:=NewExportTypedMaskGeometrySource(s.Reader,[]v2input.Entry{parent});if createErr!=nil{return zero,createErr};source.authority=s.MaskAuthority
   geometry,err=source.ResolveExportMaskGeometry(ctx,ref)
  }else{
   if parent.Contract=="ResolvedStyle"&&s.ResolvedOrigin==nil{return zero,v2input.ErrInvalid}
   geometry,err=(ExportResolvedMaskGeometrySource{Reader:s.Reader,Warps:s.Warps,ParamsAuthority:s.ParamsAuthority,ReportAuthority:s.ReportAuthority,Origin:s.ResolvedOrigin,Parents:[]v2input.Entry{parent}}).ResolveExportMaskGeometry(ctx,ref)
  }
  if err!=nil{return zero,err}
  if !validExportMaskGeometry(geometry,ref)||(found&&result!=geometry){return zero,v2input.ErrInvalid};result=geometry;found=true
 }
 if !found{return zero,v2input.ErrInvalid};return result,ctx.Err()
}
var _ ExportMaskGeometrySource=ExportAllMaskGeometrySource{}
