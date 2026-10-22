package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type ImportNativeOperationInstallation struct{Scratch *ExportArchiveScratch;Probe ExportNativeProbe;Models map[string]string;RuntimeSHA string}
type nativeImportOperationQualifier struct{native *NativeExportStyleValidator;policy ImportLayerOperationQualifier}
func nativeImportOperationSupported(key ImportOperationVersion)bool{
 if key.Version!="1"{return false}
 switch key.Operation{case "defects.auto_repair","sensor.dust_auto_repair","image.upscale","image.denoise","geometry.body","image.local","color.stats","defects.repair","portrait.detail","portrait.hair","portrait.clothing":return true};return false
}
func(i ImportNativeOperationInstallation)Build(ctx context.Context,base *ImportOperationRouter)(*ImportOperationRouter,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if base==nil||len(base.operations)==0{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(i.Scratch,i.Probe,i.Models,i.RuntimeSHA,exportCompositionStagingValidator{});if err!=nil{return nil,err};native.contract="OperationParams"
 installed:=make(map[ImportOperationVersion]ImportLayerOperationQualifier,len(base.operations))
 for key,policy:=range base.operations{
  if nativeImportOperationSupported(key){installed[key]=nativeImportOperationQualifier{native:native,policy:policy}}else{installed[key]=policy}
 }
 return NewImportOperationRouter(ctx,installed)
}
func(q nativeImportOperationQualifier)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.native==nil||q.policy==nil||!nativeImportOperationSupported(ImportOperationVersion{operation,version})||len(raw)<2||len(raw)>1<<20{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 // The exact operation comes from the installed router, never shape inference.
 // Source/dependency/runtime admission remains independent of native schema.
 if err:=q.policy.QualifyImportedLayerOperation(ctx,operation,version,append([]byte(nil),body...),binding);err!=nil{return err}
 bundle,err:=canonical.Marshal(struct{Schema int `json:"schema_version"`;Operation string `json:"operation"`;Version string `json:"version"`;Params exportCompositionDocument `json:"params"`}{1,operation,version,exportCompositionDocument{SHA:rawSHA(body),Body:json.RawMessage(body)}});if err!=nil{return err}
 sha:=rawSHA(bundle)
 entry:=v2input.Entry{Contract:"OperationParams",Ref:v2input.Ref{ArtifactID:uuid.NewSHA1(uuid.NameSpaceOID,[]byte("interchange:validate-operation:"+sha)),SHA256:sha}}
 if _,err:=q.native.ExportDocumentChildren(ctx,entry,bundle);err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if err:=q.policy.QualifyImportedLayerOperation(ctx,operation,version,append([]byte(nil),body...),binding);err!=nil{return err}
 return ctx.Err()
}
