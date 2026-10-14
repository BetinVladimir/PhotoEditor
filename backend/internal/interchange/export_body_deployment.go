package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
// Files are operator-provisioned paths, never fields of an export request.
type ExportBodyDeploymentFiles struct{Person,Dependencies,Alignment,Detector string}
type ExportBodyDeploymentRuntime struct{
 Scratch *ExportArchiveScratch
 StyleProbe,MaskProbe,ArrayProbe ExportNativeProbe
 BodySHA,FitSHA,DependencySHA,RegistrationSHA,MaskOriginSHA,MaskArraySHA,DetectorSHA,SourceArraySHA string
}
func LoadExportBodyInstallation(ctx context.Context,files ExportBodyDeploymentFiles,r ExportBodyDeploymentRuntime)(SQLExportBodyOriginInstallation,error){
 var zero SQLExportBodyOriginInstallation
 if err:=ctx.Err();err!=nil{return zero,err}
 if files.Person==""||files.Dependencies==""||files.Alignment==""||files.Detector==""||r.Scratch==nil||r.StyleProbe==nil||r.MaskProbe==nil||r.ArrayProbe==nil{return zero,v2input.ErrInvalid}
 person,err:=LoadExportStatsPersonPolicyConfig(ctx,files.Person);if err!=nil{return zero,err}
 for _,sha:=range []string{r.BodySHA,r.FitSHA,r.DependencySHA,r.RegistrationSHA,r.MaskOriginSHA,r.MaskArraySHA,r.DetectorSHA,r.SourceArraySHA}{if !validImportRef(v2input.Ref{ArtifactID:person.Report.ArtifactID,SHA256:sha}){return zero,v2input.ErrInvalid}}
 configs,err:=LoadExportBodyDependencyPolicyConfig(ctx,files.Dependencies);if err!=nil{return zero,err}
 alignment,err:=LoadExportAlignmentPolicyConfig(ctx,files.Alignment);if err!=nil{return zero,err}
 detector,err:=LoadExportDetectorPolicyConfig(ctx,files.Detector);if err!=nil{return zero,err}
 features:=[]ExportBodyDependencyPolicyConfig{}
 for _,config:=range configs{if config.Expected.Operation=="pose.detect"||config.Expected.Operation=="face.landmarks"{features=append(features,config)}}
 if len(features)!=2{return zero,v2input.ErrInvalid}
 fit:=SQLExportBodyFitInstallation{
  Alignment:SQLExportAlignmentInstallation{Policy:alignment,Scratch:r.Scratch,StyleProbe:r.StyleProbe,MaskProbe:r.MaskProbe,RegistrationRuntimeSHA:r.RegistrationSHA,MaskOriginRuntimeSHA:r.MaskOriginSHA,MaskArrayRuntimeSHA:r.MaskArraySHA},
  Features:SQLExportBodyFitFeatureInstallation{Configs:features,Detector:SQLExportDetectorInstallation{Policy:detector,Scratch:r.Scratch,Probe:r.StyleProbe,RuntimeSHA:r.DetectorSHA},Scratch:r.Scratch,Probe:r.StyleProbe,RuntimeSHA:r.DependencySHA},
  Person:person,Scratch:r.Scratch,StyleProbe:r.StyleProbe,MaskProbe:r.MaskProbe,FitRuntimeSHA:r.FitSHA,MaskArrayRuntimeSHA:r.MaskArraySHA,
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 return SQLExportBodyOriginInstallation{Dependencies:SQLExportBodyDependencyInstallation{Configs:configs,Scratch:r.Scratch,Probe:r.StyleProbe,RuntimeSHA:r.DependencySHA},Fit:fit,Scratch:r.Scratch,StyleProbe:r.StyleProbe,MaskProbe:r.MaskProbe,BodyRuntimeSHA:r.BodySHA,MaskArrayRuntimeSHA:r.MaskArraySHA,ArrayProbe:r.ArrayProbe,SourceArrayRuntimeSHA:r.SourceArraySHA},nil
}
