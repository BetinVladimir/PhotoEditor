package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Must validate original face.detect Params/result/current policy and the exact
// FaceBoxes source/geometry/physical input ancestry; document readability is insufficient.
type ExportBodyFitFaceBoxesAuthority interface{
 QualifyExportBodyFitFaceBoxes(context.Context,ExportBodyDependencyOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Entry,[]byte,ExportClosureNode,v2input.Entry)error
}
type BoundExportBodyFitFeatureOriginal struct{
 Source ExportBodyFitFeatureOriginSource
 Images ExportClosureNodeResolver
 Warps ExportDenseWarpOccurrenceAuthority
 Faces ExportBodyFitFaceBoxesAuthority
}
func(p BoundExportBodyFitFeatureOriginal)QualifyExportBodyFitFeatureOriginal(ctx context.Context,origin ExportBodyDependencyOrigin,parent v2input.Entry,raw []byte,fit ExportClosureNode,child v2input.Entry,params []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if p.Source==nil||p.Images==nil||parent.Contract!="Params"||!validImportRef(parent.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256||len(params)<2||len(params)>1<<20||rawSHA(params)!=origin.Params.SHA256{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...);original:=append([]byte(nil),params...);fit=ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}}
 edge,err:=exportBodyFitFeatureEdge(fit,child);if err!=nil{return err};if origin.Producer.Operation!=edge.Operation||origin.Producer.Version!="1"{return v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(original);if err!=nil||!bytes.Equal(normalized,original){return v2input.ErrInvalid}
 var refs struct{Image v2input.Ref `json:"image"`;Faces *v2input.Ref `json:"faces"`}
 if json.Unmarshal(original,&refs)!=nil||refs.Image!=edge.Image{return v2input.ErrInvalid}
 if edge.Operation=="face.landmarks"{if refs.Faces==nil||!validImportRef(*refs.Faces)||p.Faces==nil{return v2input.ErrInvalid}}else if refs.Faces!=nil{return v2input.ErrInvalid}
 before,err:=p.Source.LookupExportBodyFitFeatureOrigin(ctx,parent,append([]byte(nil),body...),clone(),child);if err!=nil{return err};if before!=origin{return v2input.ErrInvalid}
 imageEntry:=v2input.Entry{Ref:edge.Image,Contract:"WorkingImage"}
 node,err:=p.Images.ResolveExportNode(ctx,imageEntry);if err!=nil{return err}
 document,err:=ownExportCompositionDocument(ctx,node,imageEntry);if err!=nil{return err}
 image,err:=review.ParseWorkingImage(document.Body);if err!=nil{return err}
 if image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Canvas.Width*image.Canvas.Height>40000000{return v2input.ErrInvalid}
 if err:=qualifyExportResolvedSourceWarps(ctx,node,p.Warps);err!=nil{return err}
 if refs.Faces!=nil{
  owned:=ExportClosureNode{Entry:imageEntry,Body:append([]byte(nil),document.Body...),Children:append([]v2input.Entry(nil),node.Children...)}
  if err:=p.Faces.QualifyExportBodyFitFaceBoxes(ctx,origin,parent,append([]byte(nil),body...),clone(),child,append([]byte(nil),original...),owned,v2input.Entry{Contract:"FaceBoxes",Ref:*refs.Faces});err!=nil{return err}
 }
 finalImage,err:=p.Images.ResolveExportNode(ctx,imageEntry);if err!=nil{return err}
 finalDocument,err:=ownExportCompositionDocument(ctx,finalImage,imageEntry);if err!=nil{return err};if !bytes.Equal(finalDocument.Body,document.Body){return v2input.ErrInvalid}
 if err:=qualifyExportResolvedSourceWarps(ctx,finalImage,p.Warps);err!=nil{return err}
 after,err:=p.Source.LookupExportBodyFitFeatureOrigin(ctx,parent,append([]byte(nil),body...),clone(),child);if err!=nil{return err};if after!=origin{return v2input.ErrInvalid};return ctx.Err()
}
var _ ExportBodyFitFeatureOriginalAuthority=BoundExportBodyFitFeatureOriginal{}
