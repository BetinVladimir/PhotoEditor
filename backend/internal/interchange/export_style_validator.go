package interchange

import (
 "bytes"
 "context"
 "encoding/hex"
 "os"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type NativeExportStyleValidator struct{
 scratch *ExportArchiveScratch
 probe ExportNativeProbe
 policy []byte
 runtime string
 contract string
 authority ExportTypedDocumentValidator
}
// Authority must qualify current source/background/model/runtime scope and
// derive complete typed children. Schema success alone never replaces it.
func NewNativeExportStyleValidator(scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string,authority ExportTypedDocumentValidator)(*NativeExportStyleValidator,error){
 if scratch==nil||probe==nil||authority==nil||models==nil||len(models)>1024{return nil,v2input.ErrInvalid}
 owned:=map[string]string{}
 for key,value:=range models{if len(key)<1||len(key)>240||len(value)<1||len(value)>240{return nil,v2input.ErrInvalid};owned[key]=value}
 policy,err:=canonical.Marshal(owned);if err!=nil||len(policy)>1<<20{return nil,v2input.ErrInvalid}
 // Reuse digest validation without introducing a dummy artifact identity.
 digest,err:=hex.DecodeString(runtime);if err!=nil||len(digest)!=32||hex.EncodeToString(digest)!=runtime{return nil,v2input.ErrInvalid}
 return &NativeExportStyleValidator{scratch:scratch,probe:probe,policy:policy,runtime:runtime,contract:"StyleV2",authority:authority},nil
}
func(s *NativeExportStyleValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)(children []v2input.Entry,err error){
 if err=ctx.Err();err!=nil{return}
 if s==nil||s.authority==nil{return nil,v2input.ErrInvalid}
 if entry.Contract!=s.contract{return s.authority.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))}
 switch s.contract{case "StyleV2","Override","RenderComposition","ResolvedStyle","ResolvedOrigin","OperationParams","SeriesCorrections","SeriesOrigin","SeriesMeasureOrigin","MaskOrigin","BodyOrigin","BodyDependencyOrigin","BodyFitOrigin","PairAlignmentOrigin","FaceDetectorOrigin":default:return nil,v2input.ErrInvalid}
 maximum:=8<<20;if s.contract=="BodyFitOrigin"{maximum=16<<20};if (s.contract=="BodyDependencyOrigin"||s.contract=="PairAlignmentOrigin"){maximum=4<<20};if s.contract=="BodyOrigin"{maximum=10<<20};if s.contract=="MaskOrigin"{maximum=12<<20};if s.contract=="SeriesMeasureOrigin"{maximum=10<<20};if s.contract=="SeriesOrigin"{maximum=32<<20};if s.contract=="OperationParams"{maximum=2<<20};if s.contract=="ResolvedOrigin"{maximum=10<<20};if s.contract=="RenderComposition"{maximum=128<<20}
 if s.scratch==nil||s.probe==nil||!validImportRef(entry.Ref)||!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:s.runtime})||len(raw)<2||len(raw)>maximum||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 normalized,normalizeErr:=canonical.Bytes(body);if normalizeErr!=nil||!bytes.Equal(normalized,body){return nil,v2input.ErrInvalid}
 reserved:=int64(len(body)+len(s.policy));if !s.scratch.reserve(reserved){return nil,v2input.ErrInvalid}
 paths:=[]string{}
 defer func(){
  removed:=true
  for _,path:=range paths{if removeErr:=os.Remove(path);removeErr!=nil{removed=false;if err==nil{err=removeErr}}}
  if removed{s.scratch.release(reserved)}
  if err!=nil{children=nil}
 }()
 stage:=func(prefix string,data []byte)(string,error){
  file,createErr:=os.CreateTemp(s.scratch.root,prefix);if createErr!=nil{return "",createErr};paths=append(paths,file.Name())
  count,writeErr:=file.Write(data);closeErr:=file.Close();if writeErr!=nil{return "",writeErr};if closeErr!=nil{return "",closeErr};if count!=len(data){return "",v2input.ErrInvalid};return file.Name(),ctx.Err()
 }
 stylePath,stageErr:=stage("native-style-*.json",body);if stageErr!=nil{return nil,stageErr}
 policyPath,stageErr:=stage("native-style-policy-*.json",s.policy);if stageErr!=nil{return nil,stageErr}
 policySHA:=rawSHA(s.policy)
 requestBody:=map[string]any{"schema_version":1,"file_path":stylePath,"bytes":len(body),"sha256":entry.Ref.SHA256,"model_policy_path":policyPath,"model_policy_bytes":len(s.policy),"model_policy_sha256":policySHA}
 if s.contract=="Override"{requestBody["schema_version"]=2;requestBody["contract"]="Override"}
 if s.contract=="RenderComposition"{requestBody["schema_version"]=3;requestBody["contract"]="RenderComposition"}
 if s.contract=="ResolvedStyle"{requestBody["schema_version"]=4;requestBody["contract"]="ResolvedStyle"}
 if s.contract=="ResolvedOrigin"{requestBody["schema_version"]=5;requestBody["contract"]="ResolvedOrigin"}
 if s.contract=="OperationParams"{requestBody["schema_version"]=6;requestBody["contract"]="OperationParams"}
 if s.contract=="SeriesCorrections"{requestBody["schema_version"]=7;requestBody["contract"]="SeriesCorrections"}
 if s.contract=="SeriesOrigin"{requestBody["schema_version"]=8;requestBody["contract"]="SeriesOrigin"}
 if s.contract=="SeriesMeasureOrigin"{requestBody["schema_version"]=9;requestBody["contract"]="SeriesMeasureOrigin"}
 if s.contract=="MaskOrigin"{requestBody["schema_version"]=10;requestBody["contract"]="MaskOrigin"}
 if s.contract=="BodyOrigin"{requestBody["schema_version"]=11;requestBody["contract"]="BodyOrigin"}
 if s.contract=="BodyDependencyOrigin"{requestBody["schema_version"]=12;requestBody["contract"]="BodyDependencyOrigin"}
 if s.contract=="BodyFitOrigin"{requestBody["schema_version"]=13;requestBody["contract"]="BodyFitOrigin"}
 if s.contract=="PairAlignmentOrigin"{requestBody["schema_version"]=14;requestBody["contract"]="PairAlignmentOrigin"}
 if s.contract=="FaceDetectorOrigin"{requestBody["schema_version"]=15;requestBody["contract"]="FaceDetectorOrigin"}
 request,marshalErr:=canonical.Marshal(requestBody);if marshalErr!=nil{return nil,marshalErr}
 receipt,probeErr:=s.probe.RunExportNativeProbe(ctx,request);if probeErr!=nil{return nil,probeErr}
 if err=ctx.Err();err!=nil{return}
 if _,err=decodeExportNativeStyleReceipt(receipt,entry,int64(len(body)),policySHA,s.runtime);err!=nil{return}
 result,authorityErr:=s.authority.ExportDocumentChildren(ctx,entry,body);if authorityErr!=nil{return nil,authorityErr}
 if s.contract=="ResolvedStyle"{if err=validateExportResolvedChildren(entry,body,result);err!=nil{return nil,err}}
 if err=ctx.Err();err!=nil{return};return append([]v2input.Entry(nil),result...),nil
}
var _ ExportTypedDocumentValidator=(*NativeExportStyleValidator)(nil)

// Reuses bounded staging and exact receipts; source and nested provenance remain
// mandatory authority checks after the complete worker OverrideV1 schema.
func NewNativeExportOverrideValidator(scratch *ExportArchiveScratch, probe ExportNativeProbe, models map[string]string, runtime string, authority ExportTypedDocumentValidator) (*NativeExportStyleValidator,error) {
 validator,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,authority);if err!=nil{return nil,err}
 validator.contract="Override"
 return validator,nil
}

// Complete production ResolvedStyle validation precedes scoped source and
// producer authority. No model policy or reference is inferred from the file.
func NewNativeExportResolvedValidator(scratch *ExportArchiveScratch, probe ExportNativeProbe, models map[string]string, runtime string, authority ExportTypedDocumentValidator) (*NativeExportStyleValidator,error) {
 validator,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,authority);if err!=nil{return nil,err}
 validator.contract="ResolvedStyle"
 return validator,nil
}

func NewNativeExportSeriesValidator(scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string,authority ExportTypedDocumentValidator)(*NativeExportStyleValidator,error){
 validator,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,authority);if err!=nil{return nil,err};validator.contract="SeriesCorrections";return validator,nil
}
