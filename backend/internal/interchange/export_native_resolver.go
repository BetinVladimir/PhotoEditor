package interchange

import (
 "context"
 "photoeditor/backend/internal/v2input"
)

// Immutable installation config. Every full inventory pass gets fresh geometry
// discovery and its own resolver; no request scope or mutable cache is shared.
type ExportNativeResolverInstallation struct{
 Catalog ExportDocumentCatalog
 Objects ImportDocumentObjects
 JSON ExportClosureNodeResolver
 Scratch *ExportArchiveScratch
 ImageProbe,MaskProbe,ArrayProbe ExportNativeProbe
 ImageRuntimeSHA,ArrayRuntimeSHA string
 MaxBytes int64
 Backgrounds ExportStyleBackgroundAuthority
}

// Binary resolution requires a bound graph. JSON reads remain available for
// discovery, without silently authorizing a binary through an unbound adapter.
func(s ExportNativeResolverInstallation)ResolveExportNode(ctx context.Context,entry v2input.Entry)(ExportClosureNode,error){
 if err:=ctx.Err();err!=nil{return ExportClosureNode{},err}
 if s.JSON==nil||exportBinaryContract(entry.Contract){return ExportClosureNode{},v2input.ErrInvalid}
 return s.JSON.ResolveExportNode(ctx,entry)
}
func(s ExportNativeResolverInstallation)BindExportGraph(ctx context.Context,compiled CompiledExportPlan)(ExportClosureNodeResolver,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Catalog==nil||s.Objects==nil||s.JSON==nil||s.Scratch==nil||s.ImageProbe==nil||s.MaskProbe==nil||s.ArrayProbe==nil||s.MaxBytes<1||s.MaxBytes>128<<30||
  !validImportRef(v2input.Ref{ArtifactID:compiled.task,SHA256:s.ImageRuntimeSHA})||!validImportRef(v2input.Ref{ArtifactID:compiled.task,SHA256:s.ArrayRuntimeSHA}){return nil,v2input.ErrInvalid}
 authority,ok:=s.Catalog.(ExportWorkingArrayAuthority);if !ok{return nil,v2input.ErrInvalid}
 warpAuthority,ok:=s.Catalog.(ExportDenseWarpOccurrenceAuthority);if !ok{return nil,v2input.ErrInvalid}
 parents,defects,resolved,inlineParents,err:=discoverExportNativeParents(ctx,compiled,s.JSON);if err!=nil{return nil,err}
 arrays,err:=NewExportTypedArrayGeometrySource(s.JSON,parents);if err!=nil{return nil,err}
 arrays.workingAuthority=authority
 arrays.warpAuthority=warpAuthority
 frameAuthority,ok:=s.JSON.(ExportWorkingArrayOccurrenceAuthority);if !ok{return nil,v2input.ErrInvalid}
 arrays.frameAuthority=frameAuthority
 masks:=[]v2input.Entry{};for _,parent:=range parents{if parent.Contract=="MaskRef"{masks=append(masks,parent)}}
 qualifiers:=ExportNativeQualifierRouter{
  Images:NativeExportImageQualifier{Objects:s.Objects,Scratch:s.Scratch,Probe:s.ImageProbe,RuntimeSHA:s.ImageRuntimeSHA},
  Arrays:NativeExportArrayQualifier{Objects:s.Objects,Scratch:s.Scratch,Probe:s.ArrayProbe,Geometry:arrays,RuntimeSHA:s.ArrayRuntimeSHA},
 }
 if len(resolved)>0{
  lutAuthority,ok:=s.Catalog.(ExportLUTOccurrenceAuthority);if !ok{return nil,v2input.ErrInvalid}
  lutGeometry,err:=NewExportTypedLUTGeometrySource(s.JSON,resolved,lutAuthority);if err!=nil{return nil,err}
  qualifiers.LUTs=NativeExportArrayQualifier{Objects:s.Objects,Scratch:s.Scratch,Probe:s.ArrayProbe,Geometry:lutGeometry,RuntimeSHA:s.ArrayRuntimeSHA}
 }
 if len(defects)>0{
  labelAuthority,ok:=s.Catalog.(ExportDefectLabelOccurrenceAuthority);if !ok{return nil,v2input.ErrInvalid}
  qualifiers.DefectLabels=ExportDefectLabelQualifier{Objects:s.Objects,Source:ExportTypedDefectLabelSource{
   Reader:s.JSON,Proposals:append([]v2input.Entry(nil),defects...),Authority:labelAuthority}}
 }
 frozenParents:=[]v2input.Entry{};for _,root:=range compiled.roots{if root.Contract=="FrozenRenderRecipe"{frozenParents=append(frozenParents,root)}}
 if s.Backgrounds!=nil{qualifiers.Backgrounds=ExportBackgroundQualifier{Reader:s.JSON,Parents:frozenParents,Authority:s.Backgrounds,Images:qualifiers.Images}}
 if len(masks)>0||len(inlineParents)>0{
  maskAuthority,ok:=s.Catalog.(ExportMaskArrayAuthority);if !ok{return nil,v2input.ErrInvalid}
  var origin ExportResolvedOriginSource
  if len(inlineParents)>0{var ok bool;origin,ok=s.Catalog.(ExportResolvedOriginSource);if !ok{return nil,v2input.ErrInvalid}}
  paramsAuthority,ok:=s.Catalog.(ExportParamsMaskAuthority);if !ok{return nil,v2input.ErrInvalid}
  geometry:=ExportAllMaskGeometrySource{Warps:warpAuthority,ParamsAuthority:paramsAuthority,Reader:s.JSON,Parents:append(append([]v2input.Entry(nil),masks...),inlineParents...),MaskAuthority:maskAuthority,ResolvedOrigin:origin}
  qualifiers.Masks=NativeExportMaskQualifier{Objects:s.Objects,Scratch:s.Scratch,Probe:s.MaskProbe,Geometry:geometry,RuntimeSHA:s.ArrayRuntimeSHA}
 }
 return ExportNodeResolverRouter{JSON:s.JSON,Binary:ExportBinaryNodeResolver{Catalog:s.Catalog,Objects:s.Objects,Qualifier:qualifiers,MaxBytes:s.MaxBytes}},ctx.Err()
}
var _ ExportClosureNodeResolver=ExportNativeResolverInstallation{}
var _ ExportGraphResolverFactory=ExportNativeResolverInstallation{}
