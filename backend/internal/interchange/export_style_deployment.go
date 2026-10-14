package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
// Base installs the remaining independently qualified document semantics.
// Policy files and runtime fingerprints are selected by deployment operators.
type ExportStyleDeploymentAuthority struct {
 Base ExportDocumentValidatorInstallation
 Models map[string]string
 StyleRuntimeSHA string
 PromptFile string
 BodyFiles ExportBodyDeploymentFiles
 BodyRuntime ExportBodyDeploymentRuntime
 Backgrounds ExportStyleBackgroundAuthority
}
func(s ExportStyleDeploymentAuthority)BuildNativeExportAuthority(ctx context.Context,native ExportNativeResolverInstallation)(ExportDocumentValidatorFactory,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Base.Other==nil||s.Base.Semantics==nil||native.Scratch==nil||native.ImageProbe==nil||native.MaskProbe==nil||native.ArrayProbe==nil||s.PromptFile==""||!validImportRefForSHA(s.StyleRuntimeSHA){return nil,v2input.ErrInvalid}
 // Derive the style process from the same pinned Python environment, without
 // accepting an archive-supplied executable/module or alternate scratch pool.
 image,ok:=native.ImageProbe.(PythonExportNativeProbe);if !ok{return nil,v2input.ErrInvalid}
 style:=PythonExportStyleProbe{Python:image.Python,WorkerRoot:image.WorkerRoot,Timeout:image.Timeout}
 runtime:=s.BodyRuntime
 if runtime.Scratch!=nil||runtime.StyleProbe!=nil||runtime.MaskProbe!=nil||runtime.ArrayProbe!=nil{return nil,v2input.ErrInvalid}
 if runtime.SourceArraySHA!=""&&runtime.SourceArraySHA!=native.ArrayRuntimeSHA{return nil,v2input.ErrInvalid}
 runtime.Scratch=native.Scratch;runtime.StyleProbe=style;runtime.MaskProbe=native.MaskProbe;runtime.ArrayProbe=native.ArrayProbe;runtime.SourceArraySHA=native.ArrayRuntimeSHA
 body,err:=LoadExportBodyInstallation(ctx,s.BodyFiles,runtime);if err!=nil{return nil,err}
 prompt,err:=LoadExportPromptMaskPolicyConfig(ctx,s.PromptFile);if err!=nil{return nil,err}
 models:=map[string]string{}
 for name,sha:=range s.Models{if name==""||!validImportRefForSHA(sha){return nil,v2input.ErrInvalid};models[name]=sha}
 required,err:=exportBodyDependencyNativeModels(body.Dependencies.Configs);if err!=nil{return nil,err}
 required["lraspp_mobilenet_v3_large"]=body.Fit.Person.Expected.ModelLockSHA
 required["mobilesam"]=prompt.Expected.ModelLockSHA
 required["sfd"]=body.Fit.Features.Detector.Policy.Expected.ModelLockSHA
 for name,sha:=range required{if previous,ok:=models[name];ok&&previous!=sha{return nil,v2input.ErrInvalid};models[name]=sha}
 base:=s.Base
 locks:=map[string]bool{}
 base.MaskModelLocks=[]string{}
 for _,lock:=range append(append([]string(nil),s.Base.MaskModelLocks...),body.Fit.Person.Expected.ModelLockSHA,prompt.Expected.ModelLockSHA){
  if !validImportRefForSHA(lock){return nil,v2input.ErrInvalid}
  if !locks[lock]{locks[lock]=true;base.MaskModelLocks=append(base.MaskModelLocks,lock)}
 }
 if err:=ctx.Err();err!=nil{return nil,err}
 return ExportStyleValidatorInstallation{Base:base,Scratch:native.Scratch,Probe:style,Models:models,RuntimeSHA:s.StyleRuntimeSHA,PersonPolicy:body.Fit.Person,PromptPolicy:prompt,Body:body,Backgrounds:s.Backgrounds},nil
}
var _ ExportNativeAuthorityFactory=ExportStyleDeploymentAuthority{}
