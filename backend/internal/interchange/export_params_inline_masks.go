package interchange
import (
 "encoding/json"
 "context"
 "bytes"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
// Closed extraction for implemented inline-mask Params shapes. The caller must
// first qualify the actual operation/schema/current source and producer. This
// function is not admission and never recursively guesses masks from JSON.
func exportParamsInlineMasks(entry v2input.Entry,raw []byte)([]json.RawMessage,error){
 if entry.Contract!="Params"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{return nil,v2input.ErrInvalid}
 if fields["base_aligned"]!=nil{
  if _,err:=v2draft.DocumentChildren(raw,"Params",entry.Ref);err!=nil{return nil,err}
  var params struct{Person json.RawMessage `json:"person"`;Regions []struct{Mask json.RawMessage `json:"mask"`} `json:"regions"`}
  if json.Unmarshal(raw,&params)!=nil{return nil,v2input.ErrInvalid}
  masks:=[]json.RawMessage{}
  if params.Person!=nil&&string(params.Person)!="null"{masks=append(masks,append(json.RawMessage(nil),params.Person...))}
  for _,region:=range params.Regions{masks=append(masks,append(json.RawMessage(nil),region.Mask...))}
  return masks,nil
 }
 if fields["regions"]!=nil&&fields["resolved"]!=nil&&fields["base_aligned"]==nil{
  if _,err:=v2draft.DocumentChildren(raw,"Params",entry.Ref);err!=nil{return nil,err}
  var params struct{Regions []struct{Mask json.RawMessage `json:"mask"`} `json:"regions"`;Protected []json.RawMessage `json:"protected"`}
  if json.Unmarshal(raw,&params)!=nil{return nil,v2input.ErrInvalid}
  masks:=[]json.RawMessage{};for _,region:=range params.Regions{masks=append(masks,append(json.RawMessage(nil),region.Mask...))};for _,mask:=range params.Protected{masks=append(masks,append(json.RawMessage(nil),mask...))}
  return masks,nil
 }
 key:=""
 operation,err:=exportParamsOperationShape(raw);if err!=nil{return nil,err}
 if operation=="geometry.body"{
  if _,err:=v2draft.DocumentChildren(raw,"Params",entry.Ref);err!=nil{return nil,err};key="person"
 }else if operation=="color.stats"{
  if _,err:=review.ParseStatsParams(raw);err!=nil{return nil,err};key="person"
 }else if fields["proposal"]!=nil&&fields["review"]!=nil&&fields["resolved"]!=nil{
  if _,err:=v2draft.DocumentChildren(raw,"Params",entry.Ref);err!=nil{return nil,err};key="protected"
 }
 if key==""{return nil,nil}
 mask,err:=review.ParseMaskRef(fields[key]);if err!=nil{return nil,err}
 children,err:=v2draft.DocumentChildren(raw,"Params",entry.Ref);if err!=nil{return nil,err}
 edge:=false
 for _,child:=range children{if child.Ref.ArtifactID==mask.Artifact.ID{
  if child.Ref.SHA256!=mask.Artifact.SHA256||child.Contract!="MaskPixels"{return nil,v2input.ErrInvalid};edge=true
 }}
 if !edge{return nil,v2input.ErrInvalid}
 return []json.RawMessage{append(json.RawMessage(nil),fields[key]...)},nil
}

func qualifyExportParamsMaskSource(ctx context.Context,reader ExportClosureNodeResolver,warps ExportDenseWarpOccurrenceAuthority,parent ExportClosureNode,masks []json.RawMessage)error{
 if err:=ctx.Err();err!=nil{return err}
 if reader==nil||len(masks)==0{return v2input.ErrInvalid}
 var params struct{Image v2input.Ref `json:"image"`}
 if json.Unmarshal(parent.Body,&params)!=nil||!validImportRef(params.Image){return v2input.ErrInvalid}
 imageEntry:=v2input.Entry{Ref:params.Image,Contract:"WorkingImage"}
 edge:=false;for _,child:=range parent.Children{if child.Ref.ArtifactID==params.Image.ArtifactID{if child!=imageEntry{return v2input.ErrInvalid};edge=true}}
 if !edge{return v2input.ErrInvalid}
 source,err:=reader.ResolveExportNode(ctx,imageEntry);if err!=nil{return err}
 if source.Entry!=imageEntry||rawSHA(source.Body)!=params.Image.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(source.Body);if err!=nil{return err}
 sourceChain,err:=canonical.Bytes(image.TransformChain);if err!=nil{return err}
 for _,body:=range masks{
  mask,err:=review.ParseMaskRef(body);if err!=nil{return err}
  chain,err:=canonical.Bytes(mask.TransformChain);if err!=nil{return err}
  if mask.Canvas!=image.Canvas||!bytes.Equal(chain,sourceChain){return v2input.ErrInvalid}
 }
 return qualifyExportResolvedSourceWarps(ctx,source,warps)
}
