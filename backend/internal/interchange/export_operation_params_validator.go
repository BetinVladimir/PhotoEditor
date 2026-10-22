package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
// Source must prove exact original Params and canonical current completed
// operation/attempt/fence in the accepted project, independently of shape.
type ExportParamsOperationEvidence struct {
 Task uuid.UUID
 Operation,Version string
 Attempt int
 Fence int64
}
type ExportParamsOperationSource interface{LookupExportParamsOperation(context.Context,v2input.Entry,[]byte)(ExportParamsOperationEvidence,error)}
type ExportOperationParamsValidator struct{Other ExportTypedDocumentValidator;Source ExportParamsOperationSource;native *NativeExportStyleValidator}
func NewExportOperationParamsValidator(other ExportTypedDocumentValidator,source ExportParamsOperationSource,scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string)(*ExportOperationParamsValidator,error){
 if other==nil||source==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportCompositionStagingValidator{});if err!=nil{return nil,err};native.contract="OperationParams"
 return &ExportOperationParamsValidator{Other:other,Source:source,native:native},nil
}
func exportParamsOperationShape(raw []byte)(string,error){
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{return "",v2input.ErrInvalid}
 // Automatic repair is a separate policy contract. Never let malformed
 // automatic Params fall through to a reviewed or generic document path.
 var policyClaim string
 _ = json.Unmarshal(fields["policy"],&policyClaim)
 if fields["policy"]!=nil&&(policyClaim=="conservative_auto_v1"||fields["image"]!=nil&&fields["resolved"]!=nil&&(fields["proposal"]!=nil||fields["proposal_report"]!=nil||fields["protected"]!=nil)){
  var policy string
  if json.Unmarshal(fields["policy"],&policy)!=nil||policy!="conservative_auto_v1"{return "",v2input.ErrInvalid}
  operation:="sensor.dust_auto_repair"
  expected:=[]string{"policy","image","proposal","resolved","protected"}
  if fields["proposal_report"]!=nil{operation="defects.auto_repair";expected=[]string{"policy","image","proposal","proposal_report","resolved"}}
  if len(fields)!=len(expected){return "",v2input.ErrInvalid}
  for _,key:=range expected{if fields[key]==nil{return "",v2input.ErrInvalid}}
  return operation,nil
 }
 if fields["model_id"]!=nil{var model string;if json.Unmarshal(fields["model_id"],&model)!=nil{return "",v2input.ErrInvalid};if model=="scunet_color_real_psnr"{return "image.denoise",nil};if model=="realesrgan_x2"||model=="realesrgan_x4"{return "image.upscale",nil}}
 if fields["base"]!=nil&&(fields["detail"]!=nil||fields["wrinkle_strength"]!=nil){
  if fields["detail"]!=nil&&fields["wrinkle_strength"]!=nil{return "",v2input.ErrInvalid}
  if fields["detail"]!=nil{return "portrait.hair",nil};return "portrait.clothing",nil
 }
 if fields["base_aligned"]!=nil{return "image.local",nil}
 if fields["poses"]!=nil&&fields["person"]!=nil{return "geometry.body",nil}
 if fields["regions"]!=nil&&fields["resolved"]!=nil&&fields["base_aligned"]==nil{return "portrait.detail",nil}
 if fields["proposal"]!=nil&&fields["review"]!=nil&&fields["resolved"]!=nil{return "defects.repair",nil}
 if fields["person"]!=nil&&fields["poses"]==nil&&fields["base_aligned"]==nil{return "color.stats",nil}
 return "",nil
}
func(v *ExportOperationParamsValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if v==nil||v.Other==nil||v.Source==nil||v.native==nil{return nil,v2input.ErrInvalid}
 if entry.Contract!="Params"{return v.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))}
 if !validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 operation,err:=exportParamsOperationShape(body);if err!=nil{return nil,err}
 if operation==""{return v.Other.ExportDocumentChildren(ctx,entry,body)}
 if len(body)>1<<20{return nil,v2input.ErrInvalid}
 evidence,err:=v.Source.LookupExportParamsOperation(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 if evidence.Task==uuid.Nil||evidence.Operation!=operation||evidence.Version!="1"||evidence.Attempt<1||evidence.Fence<1{return nil,v2input.ErrInvalid}
 bundle,err:=canonical.Marshal(struct{Schema int `json:"schema_version"`;Operation string `json:"operation"`;Version string `json:"version"`;Params exportCompositionDocument `json:"params"`}{1,evidence.Operation,evidence.Version,exportCompositionDocument{SHA:entry.Ref.SHA256,Body:json.RawMessage(body)}});if err!=nil{return nil,err}
 if _,err:=v.native.ExportDocumentChildren(ctx,v2input.Entry{Ref:v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:rawSHA(bundle)},Contract:"OperationParams"},bundle);err!=nil{return nil,err}
 children,err:=v.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 current,err:=v.Source.LookupExportParamsOperation(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err};if current!=evidence{return nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return nil,err};return append([]v2input.Entry(nil),children...),nil
}
var _ ExportTypedDocumentValidator=(*ExportOperationParamsValidator)(nil)
