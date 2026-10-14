package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "path/filepath"
 "time"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type nativeExportDeploymentWire struct{
 Schema string `json:"schema_version"`
 Python string `json:"python"`
 WorkerRoot string `json:"worker_root"`
 ScratchRoot string `json:"scratch_root"`
 ScratchBytes int64 `json:"scratch_bytes"`
 TimeoutSeconds int `json:"timeout_seconds"`
 ImageRuntime string `json:"image_runtime_sha256"`
 ArrayRuntime string `json:"array_runtime_sha256"`
 Runtime string `json:"runtime_sha256"`
 Contracts string `json:"contracts_sha256"`
 Hardware string `json:"hardware_sha256"`
 MeasurementSHA string `json:"measurement_sha256"`
 Measurements json.RawMessage `json:"measurements"`
 MaxInputBytes int64 `json:"max_input_bytes"`
 ArchiveBytes int64 `json:"archive_bytes"`
 ExpandedBytes int64 `json:"expanded_bytes"`
 Files int `json:"files"`
}
// The immutable manifest pins configuration/report bytes. It does not attest
// loaded native binaries; deployment must independently qualify those paths.
type NativeExportDeployment struct{raw string}
func DecodeNativeExportDeployment(raw []byte,pin string)(NativeExportDeployment,error){
 var zero NativeExportDeployment
 if len(raw)<2||len(raw)>9<<20||!validImportRefForSHA(pin)||rawSHA(raw)!=pin{return zero,v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normal,raw){return zero,v2input.ErrInvalid}
 var w nativeExportDeploymentWire
 if json.Unmarshal(raw,&w)!=nil{return zero,v2input.ErrInvalid};exact,err:=canonical.Marshal(w)
 if err!=nil||!bytes.Equal(raw,exact)||w.Schema!="interchange-export-deployment/v1"||!filepath.IsAbs(w.Python)||!filepath.IsAbs(w.WorkerRoot)||!filepath.IsAbs(w.ScratchRoot)||w.TimeoutSeconds<1||w.TimeoutSeconds>600||w.ScratchBytes<23||w.ScratchBytes>128<<30||w.MaxInputBytes<1||w.MaxInputBytes>128<<30||w.ArchiveBytes<1||w.ArchiveBytes>64<<30||w.ExpandedBytes<1||w.ExpandedBytes>128<<30||w.Files<1||w.Files>30000||!validImportRefForSHA(w.ImageRuntime)||!validImportRefForSHA(w.ArrayRuntime){return zero,v2input.ErrInvalid}
 if _,err:=NewMeasuredExportResourceQualifier(w.Measurements,w.MeasurementSHA,w.Runtime,w.Contracts,w.Hardware);err!=nil{return zero,err}
 return NativeExportDeployment{raw:string(raw)},nil
}
// Caller installs schema/model/source/consent authority explicitly. This builder
// installs native probes and measured budgets without shell/import selection.
// Authority factories receive the exact native process handles and shared scratch
// budget selected by this pinned deployment. They cannot replace native ports.
type ExportNativeAuthorityFactory interface {
 BuildNativeExportAuthority(context.Context,ExportNativeResolverInstallation)(ExportDocumentValidatorFactory,error)
}
func(d NativeExportDeployment)Build(ctx context.Context,ports SQLExportExecutionInstaller)(ExportCreationPreparation,error){
 return d.build(ctx,ports,nil)
}
func(d NativeExportDeployment)BuildWithAuthority(ctx context.Context,ports SQLExportExecutionInstaller,factory ExportNativeAuthorityFactory)(ExportCreationPreparation,error){
 if factory==nil||ports.Dependencies!=nil||ports.DependencyFactory!=nil{return ExportCreationPreparation{},v2input.ErrInvalid}
 return d.build(ctx,ports,factory)
}
func(d NativeExportDeployment)build(ctx context.Context,ports SQLExportExecutionInstaller,factory ExportNativeAuthorityFactory)(ExportCreationPreparation,error){
 var zero ExportCreationPreparation
 if err:=ctx.Err();err!=nil{return zero,err}
 if ports.Native!=nil||ports.Binaries!=nil||ports.Qualification!=nil{return zero,v2input.ErrInvalid}
 checked,err:=DecodeNativeExportDeployment([]byte(d.raw),rawSHA([]byte(d.raw)));if err!=nil{return zero,err}
 var w nativeExportDeploymentWire;if json.Unmarshal([]byte(checked.raw),&w)!=nil{return zero,v2input.ErrInvalid}
 // Check required external ports before touching the scratch filesystem.
 if ports.API==nil||ports.Scheduler==nil||ports.API==ports.Scheduler||ports.Objects==nil||len(ports.Bucket)<3||len(ports.Bucket)>63||(factory==nil&&(ports.Dependencies==nil)==(ports.DependencyFactory==nil)){return zero,v2input.ErrInvalid}
 qualifier,err:=NewMeasuredExportResourceQualifier(w.Measurements,w.MeasurementSHA,w.Runtime,w.Contracts,w.Hardware);if err!=nil{return zero,err}
 scratch,err:=NewExportArchiveScratch(w.ScratchRoot,w.ScratchBytes);if err!=nil{return zero,err}
 timeout:=time.Duration(w.TimeoutSeconds)*time.Second
 ports.Native=&ExportNativeResolverInstallation{Scratch:scratch,ImageProbe:PythonExportNativeProbe{Python:w.Python,WorkerRoot:w.WorkerRoot,Timeout:timeout},MaskProbe:PythonExportMaskProbe{Python:w.Python,WorkerRoot:w.WorkerRoot,Timeout:timeout},ArrayProbe:PythonExportArrayProbe{Python:w.Python,WorkerRoot:w.WorkerRoot,Timeout:timeout},ImageRuntimeSHA:w.ImageRuntime,ArrayRuntimeSHA:w.ArrayRuntime}
 if factory!=nil{
  authority,err:=factory.BuildNativeExportAuthority(ctx,*ports.Native);if err!=nil{return zero,err};if authority==nil{return zero,v2input.ErrInvalid}
  ports.DependencyFactory=authority
 }
 ports.Qualification=qualifier;ports.MaxInputBytes=w.MaxInputBytes
 return NewSQLNativeExportCreationPreparation(ctx,ports,ExportLimits{ArchiveBytes:w.ArchiveBytes,ExpandedBytes:w.ExpandedBytes,Files:w.Files})
}
