package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Reader must perform full resolved schema/source/model/consent validation.
// Origin additionally proves inline mask membership in original resolve inputs.
type ExportResolvedMaskGeometrySource struct {
 Warps ExportDenseWarpOccurrenceAuthority
 Reader ExportClosureNodeResolver
 ParamsAuthority ExportParamsMaskAuthority
 Origin ExportResolvedOriginSource
 Parents []v2input.Entry
}
func(s ExportResolvedMaskGeometrySource)ResolveExportMaskGeometry(ctx context.Context,ref v2input.Ref)(ExportMaskGeometry,error){
 var zero,result ExportMaskGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Reader==nil||!validImportRef(ref)||len(s.Parents)==0||len(s.Parents)>v2input.MaxEntries{return zero,v2input.ErrInvalid}
 seen:=map[uuid.UUID]bool{};found:=false
 for _,parent:=range s.Parents{
  if (parent.Contract!="ResolvedStyle"&&parent.Contract!="Override"&&parent.Contract!="Params")||!validImportRef(parent.Ref)||seen[parent.Ref.ArtifactID]{return zero,v2input.ErrInvalid};seen[parent.Ref.ArtifactID]=true
  node,err:=s.Reader.ResolveExportNode(ctx,parent);if err!=nil{return zero,err}
  if node.Entry!=parent||len(node.Body)<2||len(node.Body)>8<<20||rawSHA(node.Body)!=parent.Ref.SHA256{return zero,v2input.ErrInvalid}
  if parent.Contract=="ResolvedStyle"{if s.Origin==nil{return zero,v2input.ErrInvalid};if err:=validateExportResolvedChildren(parent,node.Body,node.Children);err!=nil{return zero,err}}
  var maskBodies []json.RawMessage
  if parent.Contract=="Params"{
   maskBodies,err=exportParamsInlineMasks(parent,node.Body);if err!=nil{return zero,err}
   if len(maskBodies)>0{if err:=qualifyExportParamsMaskSource(ctx,s.Reader,s.Warps,node,maskBodies);err!=nil{return zero,err}}
  }else{
   var override struct{Masks []struct{Mask json.RawMessage `json:"mask"`} `json:"mask_refs"`}
   overrideRaw:=node.Body
   if parent.Contract=="ResolvedStyle"{var projection struct{Params struct{Override json.RawMessage `json:"override"`} `json:"params"`};if json.Unmarshal(node.Body,&projection)!=nil{return zero,v2input.ErrInvalid};overrideRaw=projection.Params.Override}
   if len(overrideRaw)==0||string(overrideRaw)=="null"{continue}
   if json.Unmarshal(overrideRaw,&override)!=nil||len(override.Masks)>128{return zero,v2input.ErrInvalid}
   for _,binding:=range override.Masks{maskBodies=append(maskBodies,binding.Mask)}
  }
  parentMatched:=false
  for _,maskBody:=range maskBodies{
   mask,err:=review.ParseMaskRef(maskBody);if err!=nil{return zero,err}
   if mask.Artifact.ID!=ref.ArtifactID{continue};if mask.Artifact.SHA256!=ref.SHA256{return zero,v2input.ErrInvalid}
   edge:=false;for _,child:=range node.Children{if child.Ref==ref&&child.Contract=="MaskPixels"{edge=true}}
   if !edge{return zero,v2input.ErrInvalid}
   canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return zero,err}
   geometry:=ExportMaskGeometry{Width:int(mask.Canvas.Width),Height:int(mask.Canvas.Height),CanvasSHA:canvas}
   if !validExportMaskGeometry(geometry,ref)||(found&&result!=geometry){return zero,v2input.ErrInvalid}
   result=geometry;found=true;parentMatched=true
  }
  if parentMatched&&parent.Contract=="Params"{if s.ParamsAuthority==nil{return zero,v2input.ErrInvalid};if err:=s.ParamsAuthority.QualifyExportParamsMask(ctx,parent,append([]byte(nil),node.Body...),ref);err!=nil{return zero,err}}
  if parentMatched&&parent.Contract=="ResolvedStyle"{if _,err:=s.Origin.LookupExportResolvedOrigin(ctx,parent,append([]byte(nil),node.Body...));err!=nil{return zero,err}}
 }
 if !found{return zero,v2input.ErrInvalid};return result,ctx.Err()
}
var _ ExportMaskGeometrySource=ExportResolvedMaskGeometrySource{}
