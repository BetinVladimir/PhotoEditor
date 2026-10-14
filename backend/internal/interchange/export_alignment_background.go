package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Must authorize the original registration's exact declared mask input, original
// mask producer/manifest/report, current model/consent and immutable object.
type ExportAlignmentBackgroundSource interface{
 LookupExportAlignmentBackground(context.Context,ExportBodyDependencyOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Entry,[]byte,ExportClosureNode)(objectstore.Object,error)
}
type NativeExportAlignmentBackgroundQualifier struct{
 Source ExportAlignmentBackgroundSource
 Objects ImportDocumentObjects
 Scratch *ExportArchiveScratch
 Probe ExportNativeProbe
 RuntimeSHA string
}
func exportAlignmentBackgroundProjection(origin ExportBodyDependencyOrigin,parent v2input.Entry,raw []byte,fit ExportClosureNode,alignment v2input.Entry,params []byte,image ExportClosureNode)(review.MaskRef,error){
 var zero review.MaskRef
 if parent.Contract!="Params"||!validImportRef(parent.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256||len(params)<2||len(params)>1<<20||rawSHA(params)!=origin.Params.SHA256||origin.Producer.Operation!="pair.register"||origin.Producer.Version!="1"{return zero,v2input.ErrInvalid}
 inventory,err:=exportBodyFitAlignmentEdge(fit,alignment);if err!=nil{return zero,err}
 var fields map[string]json.RawMessage;if json.Unmarshal(params,&fields)!=nil{return zero,v2input.ErrInvalid}
 for key:=range fields{switch key{case "before","after","seed","mode","manual_transform","manual_confirmed","background_mask":default:return zero,v2input.ErrInvalid}}
 var refs struct{Before v2input.Ref `json:"before"`;After v2input.Ref `json:"after"`}
 if json.Unmarshal(params,&refs)!=nil||refs.Before!=inventory.Before||refs.After!=inventory.After||image.Entry!=(v2input.Entry{Ref:refs.Before,Contract:"WorkingImage"}){return zero,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(params);if err!=nil||!bytes.Equal(normalized,params){return zero,v2input.ErrInvalid}
 if _,err:=ownExportCompositionDocument(context.Background(),image,image.Entry);err!=nil{return zero,err}
 working,err:=review.ParseWorkingImage(image.Body);if err!=nil{return zero,err}
 mask,err:=review.ParseMaskRef(fields["background_mask"]);if err!=nil{return zero,err}
 a,err:=canonical.Bytes(mask.TransformChain);if err!=nil{return zero,err};b,err:=canonical.Bytes(working.TransformChain);if err!=nil{return zero,err}
 if mask.Canvas!=working.Canvas||!bytes.Equal(a,b){return zero,v2input.ErrInvalid};return mask,nil
}
func(q NativeExportAlignmentBackgroundQualifier)QualifyExportAlignmentBackground(ctx context.Context,origin ExportBodyDependencyOrigin,parent v2input.Entry,raw []byte,fit ExportClosureNode,alignment v2input.Entry,params []byte,image ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Source==nil||q.Objects==nil||q.Scratch==nil||q.Probe==nil{return v2input.ErrInvalid}
 mask,err:=exportAlignmentBackgroundProjection(origin,parent,raw,fit,alignment,params,image);if err!=nil{return err}
 body:=append([]byte(nil),raw...);original:=append([]byte(nil),params...)
 fitBody:=append([]byte(nil),fit.Body...);imageBody:=append([]byte(nil),image.Body...);children:=append([]v2input.Entry(nil),image.Children...)
 lookup:=func()(objectstore.Object,error){return q.Source.LookupExportAlignmentBackground(ctx,origin,parent,append([]byte(nil),body...),ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fitBody...)},alignment,append([]byte(nil),original...),ExportClosureNode{Entry:image.Entry,Body:append([]byte(nil),imageBody...),Children:append([]v2input.Entry(nil),children...)})}
 before,err:=lookup();if err!=nil{return err}
 canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return err}
 ref:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}
 geometry:=statsMaskGeometry{reference:ref,geometry:ExportMaskGeometry{Width:int(mask.Canvas.Width),Height:int(mask.Canvas.Height),CanvasSHA:canvas}}
 native:=NativeExportMaskQualifier{Objects:q.Objects,Scratch:q.Scratch,Probe:q.Probe,Geometry:geometry,RuntimeSHA:q.RuntimeSHA}
 if err:=native.QualifyExportBinary(ctx,v2input.Entry{Ref:ref,Contract:"MaskPixels"},before);err!=nil{return err}
 after,err:=lookup();if err!=nil{return err};if before!=after{return v2input.ErrInvalid};return ctx.Err()
}
var _ ExportAlignmentBackgroundAuthority=NativeExportAlignmentBackgroundQualifier{}
