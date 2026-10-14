package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Must qualify original background-mask bytes and provenance under the exact
// original registration Params/producer, never a synthetic consumer identity.
type ExportAlignmentBackgroundAuthority interface{
 QualifyExportAlignmentBackground(context.Context,ExportBodyDependencyOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Entry,[]byte,ExportClosureNode)error
}
type BoundExportAlignmentOriginalInputs struct{
 Source ExportBodyFitAlignmentOriginSource
 // Must resolve qualified physical WorkingImages, not catalogue metadata only.
 Images ExportClosureNodeResolver
 Warps ExportDenseWarpOccurrenceAuthority
 Background ExportAlignmentBackgroundAuthority
}
func(p BoundExportAlignmentOriginalInputs)QualifyExportAlignmentOriginalInputs(ctx context.Context,origin ExportBodyDependencyOrigin,parent v2input.Entry,raw []byte,fit ExportClosureNode,alignment v2input.Entry,params []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if p.Source==nil||p.Images==nil||parent.Contract!="Params"||!validImportRef(parent.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256||len(params)<2||len(params)>1<<20||rawSHA(params)!=origin.Params.SHA256{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...);original:=append([]byte(nil),params...)
 fit=ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}}
 inventory,err:=exportBodyFitAlignmentEdge(fit,alignment);if err!=nil{return err}
 var fields map[string]json.RawMessage
 if json.Unmarshal(original,&fields)!=nil{return v2input.ErrInvalid}
 for key:=range fields{switch key{case "before","after","seed","mode","manual_transform","manual_confirmed","background_mask":default:return v2input.ErrInvalid}}
 var refs struct{Before v2input.Ref `json:"before"`;After v2input.Ref `json:"after"`}
 if json.Unmarshal(original,&refs)!=nil||refs.Before!=inventory.Before||refs.After!=inventory.After{return v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(original);if err!=nil||!bytes.Equal(normalized,original){return v2input.ErrInvalid}
 current,err:=p.Source.LookupExportBodyFitAlignmentOrigin(ctx,parent,append([]byte(nil),body...),clone(),alignment);if err!=nil{return err};if current!=origin||origin.Producer.Operation!="pair.register"||origin.Producer.Version!="1"{return v2input.ErrInvalid}
 entries:=[]v2input.Entry{{Ref:refs.Before,Contract:"WorkingImage"},{Ref:refs.After,Contract:"WorkingImage"}}
 nodes:=make([]ExportClosureNode,2);documents:=make([]exportCompositionDocument,2);images:=make([]review.WorkingImage,2)
 for i,entry:=range entries{
  node,err:=p.Images.ResolveExportNode(ctx,entry);if err!=nil{return err}
  document,err:=ownExportCompositionDocument(ctx,node,entry);if err!=nil{return err}
  image,err:=review.ParseWorkingImage(document.Body);if err!=nil{return err}
  if image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Alpha!=nil||image.Canvas.Width<8||image.Canvas.Height<8||image.Canvas.Width*image.Canvas.Height>200000000{return v2input.ErrInvalid}
  nodes[i]=ExportClosureNode{Entry:entry,Body:append([]byte(nil),document.Body...),Children:append([]v2input.Entry(nil),node.Children...)};documents[i]=document;images[i]=image
  if err:=qualifyExportResolvedSourceWarps(ctx,nodes[i],p.Warps);err!=nil{return err}
 }
 if images[0].Color!=images[1].Color{return v2input.ErrInvalid}
 maskRaw:=fields["background_mask"]
 if len(maskRaw)>0&&!bytes.Equal(maskRaw,[]byte("null")){
  if p.Background==nil{return v2input.ErrInvalid}
  mask,err:=review.ParseMaskRef(maskRaw);if err!=nil{return err}
  maskChain,err:=canonical.Bytes(mask.TransformChain);if err!=nil{return err};imageChain,err:=canonical.Bytes(images[0].TransformChain);if err!=nil{return err}
  if mask.Canvas!=images[0].Canvas||!bytes.Equal(maskChain,imageChain){return v2input.ErrInvalid}
  before:=nodes[0];before.Body=append([]byte(nil),before.Body...);before.Children=append([]v2input.Entry(nil),before.Children...)
  if err:=p.Background.QualifyExportAlignmentBackground(ctx,origin,parent,append([]byte(nil),body...),clone(),alignment,append([]byte(nil),original...),before);err!=nil{return err}
 }
 for i,entry:=range entries{
  node,err:=p.Images.ResolveExportNode(ctx,entry);if err!=nil{return err}
  document,err:=ownExportCompositionDocument(ctx,node,entry);if err!=nil{return err};if document.SHA!=documents[i].SHA||!bytes.Equal(document.Body,documents[i].Body){return v2input.ErrInvalid}
  if err:=qualifyExportResolvedSourceWarps(ctx,node,p.Warps);err!=nil{return err}
 }
 final,err:=p.Source.LookupExportBodyFitAlignmentOrigin(ctx,parent,append([]byte(nil),body...),clone(),alignment);if err!=nil{return err};if final!=origin{return v2input.ErrInvalid};return ctx.Err()
}
var _ ExportAlignmentOriginalInputAuthority=BoundExportAlignmentOriginalInputs{}
