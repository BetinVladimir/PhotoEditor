package interchange
import (
 "context"
 "os"
 "path/filepath"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
 "testing"
)
func TestStyleDeploymentRequiresQualifiedBaseAndNativePorts(t *testing.T){
 s:=ExportStyleDeploymentAuthority{}
 q,err:=s.BuildNativeExportAuthority(context.Background(),ExportNativeResolverInstallation{})
 if err==nil||q!=nil{t.Fatal("missing authority accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 q,err=s.BuildNativeExportAuthority(ctx,ExportNativeResolverInstallation{})
 if err!=context.Canceled||q!=nil{t.Fatal("cancellation lost",err)}
}

// This is configuration composition coverage; no native process or SQL runs.
func TestStyleDeploymentComposesSharedNativeBudget(t *testing.T){
 for _,mode:=range []string{"valid","model_conflict","prompt_model_conflict","invalid_mask_lock","alternate_scratch","array_pin","missing_prompt","alternate_probe"}{t.Run(mode,func(t *testing.T){
  root:=t.TempDir()
  digest:=func(s string)string{return rawSHA([]byte(s))}
  locks:=func(op string)v2qualification.DeploymentLocks{
   backend:="mps";if op=="pair.register"||op=="pair.fit.body"{backend="cpu"}
   return v2qualification.DeploymentLocks{Operation:op,Version:"1",Backend:backend,ModelLockSHA:digest(op),RuntimeLockSHA:digest("runtime"),DeviceProfileSHA:digest("device"),DatasetSHA:digest("dataset"),IssuerID:uuid.New().String(),RequiredChecks:[]string{"coverage"},CalibrationSHAs:[]string{digest("calibration")},TestFixtureSHAs:[]string{digest("test")},EvidenceSHAs:[]string{digest("evidence")},LicenseCleared:true}
  }
  report:=func(op string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(op+" report")}}
  write:=func(name string,value any)string{raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)};path:=filepath.Join(root,name);if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)};return path}
  files:=ExportBodyDeploymentFiles{
   Person:write("person.json",ExportStatsPersonPolicyConfig{Schema:1,Report:report("person"),Locks:locks("mask.person")}),
   Alignment:write("alignment.json",ExportAlignmentPolicyConfig{Schema:1,Report:report("alignment"),Locks:locks("pair.register")}),
   Detector:write("detector.json",ExportDetectorPolicyConfig{Schema:1,Report:report("detector"),Locks:locks("face.detect")}),
  }
  policies:=[]ExportBodyDependencyPolicyFileEntry{}
  for _,op:=range []string{"pose.detect","face.landmarks","pair.fit.body"}{policies=append(policies,ExportBodyDependencyPolicyFileEntry{Report:report(op),Locks:locks(op)})}
  files.Dependencies=write("dependencies.json",ExportBodyDependencyPolicyFile{Schema:1,Policies:policies})
  prompt:=write("prompt.json",ExportPromptMaskPolicyConfig{Schema:1,Report:report("prompt"),Locks:locks("mask.prompt")})
  scratch,err:=NewExportArchiveScratch(root,1<<20);if err!=nil{t.Fatal(err)}
  arraySHA:=digest("arrays")
  native:=ExportNativeResolverInstallation{Scratch:scratch,ImageProbe:PythonExportNativeProbe{Python:"/usr/bin/python3",WorkerRoot:root,Timeout:time.Second},MaskProbe:&exportStyleProbeStub{},ArrayProbe:&exportStyleProbeStub{},ArrayRuntimeSHA:arraySHA}
  s:=ExportStyleDeploymentAuthority{Base:ExportDocumentValidatorInstallation{Other:exportReaderValidator{&importValidatorStub{}},Semantics:&exportSemanticsStub{}},Models:map[string]string{},StyleRuntimeSHA:digest("style"),PromptFile:prompt,BodyFiles:files,BodyRuntime:ExportBodyDeploymentRuntime{BodySHA:digest("body"),FitSHA:digest("fit"),DependencySHA:digest("deps"),RegistrationSHA:digest("registration"),MaskOriginSHA:digest("origin"),MaskArraySHA:digest("mask"),DetectorSHA:digest("detector")}}
  switch mode{
  case "model_conflict":s.Models["fan2d"]=digest("wrong")
  case "prompt_model_conflict":s.Models["mobilesam"]=digest("wrong")
  case "invalid_mask_lock":s.Base.MaskModelLocks=[]string{"invalid"}
  case "alternate_scratch":s.BodyRuntime.Scratch=scratch
  case "array_pin":s.BodyRuntime.SourceArraySHA=digest("wrong")
  case "missing_prompt":s.PromptFile=filepath.Join(root,"absent")
  case "alternate_probe":native.ImageProbe=&exportStyleProbeStub{}
  }
  result,err:=s.BuildNativeExportAuthority(context.Background(),native)
  if mode!="valid"{if err==nil||result!=nil{t.Fatal("invalid composition accepted",mode)};return}
  if err!=nil{t.Fatal(err)}
  style,ok:=result.(ExportStyleValidatorInstallation);if !ok{t.Fatal("style factory missing")}
  body,ok:=style.Body.(SQLExportBodyOriginInstallation);if !ok{t.Fatal("body factory missing")}
  if style.Scratch!=scratch||body.Scratch!=scratch||body.Fit.Scratch!=scratch||body.Fit.Features.Scratch!=scratch||body.Fit.Alignment.Scratch!=scratch||body.Fit.Features.Detector.Scratch!=scratch{t.Fatal("scratch budget split")}
  if body.ArrayProbe!=native.ArrayProbe||body.MaskProbe!=native.MaskProbe||body.SourceArrayRuntimeSHA!=arraySHA{t.Fatal("native handles replaced")}
  if style.Models["fan2d"]!=digest("face.landmarks")||style.Models["lraspp_mobilenet_v3_large"]!=digest("mask.person"){t.Fatal("wrong qualified model map")}
  if style.Models["mobilesam"]!=digest("mask.prompt"){t.Fatal("prompt model omitted")}
  personFound,promptFound:=false,false
  for _,lock:=range style.Base.MaskModelLocks{if lock==digest("mask.person"){personFound=true};if lock==digest("mask.prompt"){promptFound=true}}
  if !personFound||!promptFound{t.Fatal("qualified mask lock omitted")}
  s.Models["fan2d"]=digest("mutated")
  if style.Models["fan2d"]!=digest("face.landmarks"){t.Fatal("model map retained caller ownership")}
 })}
}
