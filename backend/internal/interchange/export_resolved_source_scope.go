package interchange
import (
 "context"
 "bytes"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/editor"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Source scope supplements, never replaces, native schema and current
// model/consent authority. It uses actual validated canonical WorkingImages.
type ExportResolvedSourceScope struct {
 Warps ExportDenseWarpOccurrenceAuthority
 Sources ExportClosureNodeResolver
 Bindings []v2input.SessionVersionBinding
 Other ExportTypedDocumentValidator
}
func(s ExportResolvedSourceScope)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Sources==nil||s.Other==nil{return nil,v2input.ErrInvalid}
 if entry.Contract=="ResolvedStyle"&&(!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256){return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 if entry.Contract=="ResolvedStyle"{
  if !validImportRef(entry.Ref)||len(body)<2||len(body)>8<<20||rawSHA(body)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
  var projection struct{Params struct{Image v2input.Ref `json:"image"`;Override json.RawMessage `json:"override"`} `json:"params"`}
  if json.Unmarshal(body,&projection)!=nil||!validImportRef(projection.Params.Image){return nil,v2input.ErrInvalid}
  found:=false
  for _,binding:=range s.Bindings{
   if binding.Source!=projection.Params.Image{continue}
   found=true
   sourceEntry:=v2input.Entry{Ref:binding.Source,Contract:"WorkingImage"}
   source,err:=s.Sources.ResolveExportNode(ctx,sourceEntry);if err!=nil{return nil,err}
   if source.Entry!=sourceEntry||rawSHA(source.Body)!=binding.Source.SHA256{return nil,v2input.ErrInvalid}
   image,err:=review.ParseWorkingImage(source.Body);if err!=nil{return nil,err}
   canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return nil,err};if canvas!=binding.CanvasHash{return nil,v2input.ErrInvalid}
   if err:=qualifyExportResolvedSourceWarps(ctx,source,s.Warps);err!=nil{return nil,err}
   if len(projection.Params.Override)>0&&string(projection.Params.Override)!="null"{
    if err:=validateExportResolvedOverride(projection.Params.Override,image);err!=nil{return nil,err}
   }
  }
  if !found{return nil,v2input.ErrInvalid}
 }
 children,err:=s.Other.ExportDocumentChildren(ctx,entry,body);if err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err};return append([]v2input.Entry(nil),children...),nil
}
var _ ExportTypedDocumentValidator=ExportResolvedSourceScope{}

// Editor validation binds the encoded original source; every inline mask must
// additionally match the actual source canvas and complete transform chain.
func validateExportResolvedOverride(raw []byte,image review.WorkingImage)error{
 if err:=editor.ValidateOverride(raw,image.Canvas.SourceSHA256);err!=nil{return err}
 var override struct{Masks []struct{Mask json.RawMessage `json:"mask"`} `json:"mask_refs"`}
 if json.Unmarshal(raw,&override)!=nil||len(override.Masks)>128{return v2input.ErrInvalid}
 sourceChain,err:=canonical.Bytes(image.TransformChain);if err!=nil{return err}
 for _,binding:=range override.Masks{
  mask,err:=review.ParseMaskRef(binding.Mask);if err!=nil{return err}
  if mask.Canvas!=image.Canvas{return v2input.ErrInvalid}
  chain,err:=canonical.Bytes(mask.TransformChain);if err!=nil{return err}
  if !bytes.Equal(chain,sourceChain){return v2input.ErrInvalid}
 }
 return nil
}

// Inline mask chains equal this canonical source chain. Verify each field at
// its actual WorkingImage occurrence, preserving the existing SQL ancestry
// authority instead of fabricating a separately stored MaskRef parent.
func qualifyExportResolvedSourceWarps(ctx context.Context,source ExportClosureNode,authority ExportDenseWarpOccurrenceAuthority)error{
 if err:=ctx.Err();err!=nil{return err}
 if source.Entry.Contract!="WorkingImage"||!validImportRef(source.Entry.Ref)||rawSHA(source.Body)!=source.Entry.Ref.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(source.Body);if err!=nil{return err}
 var chain struct{Steps []struct{Inverse *review.ArtifactRef `json:"inverse_field"`} `json:"steps"`}
 if json.Unmarshal(image.TransformChain,&chain)!=nil{return v2input.ErrInvalid}
 for index,step:=range chain.Steps{if step.Inverse!=nil{
  if authority==nil{return v2input.ErrInvalid}
  field:=v2input.Entry{Ref:v2input.Ref{ArtifactID:step.Inverse.ID,SHA256:step.Inverse.SHA256},Contract:"DenseWarpField"}
  edge:=false
  for _,child:=range source.Children{if child.Ref.ArtifactID==field.Ref.ArtifactID{if child!=field{return v2input.ErrInvalid};edge=true}}
  if !edge{return v2input.ErrInvalid}
  if err:=authority.QualifyExportDenseWarpOccurrence(ctx,source.Entry,append([]byte(nil),source.Body...),index,field);err!=nil{return err}
 }}
 return ctx.Err()
}
