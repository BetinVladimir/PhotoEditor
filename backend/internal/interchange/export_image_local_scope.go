package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Images must be a scoped provenance-qualified reader; this adapter adds the
// cross-document geometry invariant used by LocalParams.validate_geometry.
type ExportImageLocalSourceScope struct{Images ExportClosureNodeResolver;Resolved ExportClosureNodeResolver;Warps ExportDenseWarpOccurrenceAuthority;Other ExportTypedDocumentValidator}
func(s ExportImageLocalSourceScope)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Other==nil||s.Images==nil||s.Resolved==nil{return nil,v2input.ErrInvalid}
 if entry.Contract!="Params"{return s.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))}
 if !validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 operation,err:=exportParamsOperationShape(body);if err!=nil{return nil,err}
 if operation!="image.local"{return s.Other.ExportDocumentChildren(ctx,entry,body)}
 var params struct{Image v2input.Ref `json:"image"`;Base v2input.Ref `json:"base_aligned"`;Resolved v2input.Ref `json:"resolved"`;Series *v2input.Ref `json:"series"`;Item *uuid.UUID `json:"item_id"`;Regions []struct{ID string `json:"region_id"`} `json:"regions"`}
 if json.Unmarshal(body,&params)!=nil||!validImportRef(params.Image)||!validImportRef(params.Base){return nil,v2input.ErrInvalid}
 load:=func(ref v2input.Ref)(review.WorkingImage,error){
  expected:=v2input.Entry{Ref:ref,Contract:"WorkingImage"};node,err:=s.Images.ResolveExportNode(ctx,expected);if err!=nil{return review.WorkingImage{},err}
  if node.Entry!=expected||rawSHA(node.Body)!=ref.SHA256{return review.WorkingImage{},v2input.ErrInvalid}
  image,err:=review.ParseWorkingImage(node.Body);if err!=nil{return review.WorkingImage{},err}
  if err:=qualifyExportResolvedSourceWarps(ctx,node,s.Warps);err!=nil{return review.WorkingImage{},err};return image,nil
 }
 image,err:=load(params.Image);if err!=nil{return nil,err};base,err:=load(params.Base);if err!=nil{return nil,err}
 a,err:=canonical.Bytes(image.TransformChain);if err!=nil{return nil,err};b,err:=canonical.Bytes(base.TransformChain);if err!=nil{return nil,err}
 alphaEqual:=image.Alpha==nil&&base.Alpha==nil||image.Alpha!=nil&&base.Alpha!=nil&&*image.Alpha==*base.Alpha
 if image.DType!="float32"||base.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color!=base.Color||image.Canvas!=base.Canvas||!bytes.Equal(a,b)||!alphaEqual{return nil,v2input.ErrInvalid}
 // Full native ResolvedStyle and original Params authority belong to this
 // dedicated nonrecursive reader, rather than this cross-document projection.
 if !validImportRef(params.Resolved)||(params.Series==nil)!=(params.Item==nil){return nil,v2input.ErrInvalid}
 resolvedEntry:=v2input.Entry{Ref:params.Resolved,Contract:"ResolvedStyle"}
 resolved,err:=s.Resolved.ResolveExportNode(ctx,resolvedEntry);if err!=nil{return nil,err}
 if resolved.Entry!=resolvedEntry||rawSHA(resolved.Body)!=params.Resolved.SHA256||len(resolved.Body)>8<<20{return nil,v2input.ErrInvalid}
 if err:=validateExportResolvedChildren(resolvedEntry,resolved.Body,resolved.Children);err!=nil{return nil,err}
 var style struct{Mode string `json:"mode"`;Params struct{Image v2input.Ref `json:"image"`} `json:"params"`;Modules struct{Extensions struct{Regions []struct{ID string `json:"region_id"`} `json:"region_adjustments"`} `json:"extensions"`} `json:"modules"`}
 if json.Unmarshal(resolved.Body,&style)!=nil||style.Mode=="skip"||!validImportRef(style.Params.Image){return nil,v2input.ErrInvalid}
 styleSource:=base
 if style.Params.Image!=params.Base{styleSource,err=load(style.Params.Image);if err!=nil{return nil,err}}
 if image.Color!=styleSource.Color||image.Canvas.SourceSHA256!=styleSource.Canvas.SourceSHA256{return nil,v2input.ErrInvalid}
 rules:=map[string]bool{}
 for _,rule:=range style.Modules.Extensions.Regions{if rule.ID==""||rules[rule.ID]{return nil,v2input.ErrInvalid};rules[rule.ID]=true}
 for _,region:=range params.Regions{if !rules[region.ID]{return nil,v2input.ErrInvalid}}
 masks,err:=exportParamsInlineMasks(entry,body);if err!=nil{return nil,err}
 for _,rawMask:=range masks{mask,err:=review.ParseMaskRef(rawMask);if err!=nil{return nil,err};chain,err:=canonical.Bytes(mask.TransformChain);if err!=nil{return nil,err};if mask.Canvas!=image.Canvas||!bytes.Equal(chain,a){return nil,v2input.ErrInvalid}}
 if params.Series!=nil{
  if !validImportRef(*params.Series){return nil,v2input.ErrInvalid}
  expected:=v2input.Entry{Ref:*params.Series,Contract:"SeriesCorrections"}
  series,err:=s.Resolved.ResolveExportNode(ctx,expected);if err!=nil{return nil,err}
  if series.Entry!=expected||len(series.Body)<2||len(series.Body)>8<<20||rawSHA(series.Body)!=params.Series.SHA256||len(series.Children)!=0{return nil,v2input.ErrInvalid}
  var corrections struct{Items []struct{Item uuid.UUID `json:"item_id"`} `json:"items"`}
  if json.Unmarshal(series.Body,&corrections)!=nil||len(corrections.Items)<2||len(corrections.Items)>2000{return nil,v2input.ErrInvalid}
  matches:=0;for _,item:=range corrections.Items{if item.Item==*params.Item{matches++}}
  if matches!=1{return nil,v2input.ErrInvalid}
 }
 children,err:=s.Other.ExportDocumentChildren(ctx,entry,body);if err!=nil{return nil,err};return append([]v2input.Entry(nil),children...),ctx.Err()
}
var _ ExportTypedDocumentValidator=ExportImageLocalSourceScope{}
