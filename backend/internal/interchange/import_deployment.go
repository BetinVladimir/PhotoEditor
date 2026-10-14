package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "path/filepath"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type nativeImportDeploymentWire struct{
 Schema string `json:"schema_version"`
 Python string `json:"python"`
 WorkerRoot string `json:"worker_root"`
 ScratchRoot string `json:"scratch_root"`
 ScratchBytes int64 `json:"scratch_bytes"`
 TimeoutSeconds int `json:"timeout_seconds"`
 ArrayRuntime string `json:"array_runtime_sha256"`
 Runtime string `json:"runtime_sha256"`
 Contracts string `json:"contracts_sha256"`
 Hardware string `json:"hardware_sha256"`
 MeasurementSHA string `json:"measurement_sha256"`
 Measurements json.RawMessage `json:"measurements"`
 MaxInputBytes int64 `json:"max_input_bytes"`
}
// A pinned manifest binds configuration bytes. It does not attest native
// binaries; every actual field read still requires native receipt validation.
type NativeImportDeployment struct{raw string}
func DecodeNativeImportDeployment(raw []byte,pin string)(NativeImportDeployment,error){
 var zero NativeImportDeployment
 if len(raw)<2||len(raw)>9<<20||!validImportRefForSHA(pin)||rawSHA(raw)!=pin{return zero,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return zero,v2input.ErrInvalid}
 var w nativeImportDeploymentWire
 if err:=json.Unmarshal(raw,&w);err!=nil{return zero,err}
 exact,err:=canonical.Marshal(w);if err!=nil||!bytes.Equal(exact,raw){return zero,v2input.ErrInvalid}
 if w.Schema!="interchange-import-deployment/v1"||!filepath.IsAbs(w.Python)||!filepath.IsAbs(w.WorkerRoot)||!filepath.IsAbs(w.ScratchRoot)||w.TimeoutSeconds<1||w.TimeoutSeconds>600||w.ScratchBytes<23||w.ScratchBytes>128<<30||w.MaxInputBytes<1||w.MaxInputBytes>128<<30||!validImportRefForSHA(w.ArrayRuntime){return zero,v2input.ErrInvalid}
 if _,err:=NewMeasuredImportResourceQualifier(w.Measurements,w.MeasurementSHA,w.Runtime,w.Contracts,w.Hardware);err!=nil{return zero,err}
 return NativeImportDeployment{raw:string(raw)},nil
}
func(d NativeImportDeployment)Build(ctx context.Context,db *pgxpool.Pool,objects ImportDocumentObjects,bucket string)(ImportResolverDependencies,*MeasuredImportResourceQualifier,error){
 var zero ImportResolverDependencies
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 if db==nil||objects==nil||len(bucket)<3||len(bucket)>63{return zero,nil,v2input.ErrInvalid}
 checked,err:=DecodeNativeImportDeployment([]byte(d.raw),rawSHA([]byte(d.raw)));if err!=nil{return zero,nil,err}
 var w nativeImportDeploymentWire
 if err:=json.Unmarshal([]byte(checked.raw),&w);err!=nil{return zero,nil,err}
 resources,err:=NewMeasuredImportResourceQualifier(w.Measurements,w.MeasurementSHA,w.Runtime,w.Contracts,w.Hardware);if err!=nil{return zero,nil,err}
 if err:=ValidateSQLImportContinuationInstallation(ctx,db);err!=nil{return zero,nil,err}
 scratch,err:=NewExportArchiveScratch(w.ScratchRoot,w.ScratchBytes);if err!=nil{return zero,nil,err}
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 probe:=PythonExportNativeProbe{Python:w.Python,WorkerRoot:w.WorkerRoot,Timeout:time.Duration(w.TimeoutSeconds)*time.Second}
 return ImportResolverDependencies{DB:db,Objects:objects,Bucket:bucket,MaxFileBytes:w.MaxInputBytes,WarpScratch:scratch,WarpProbe:probe,WarpRuntimeSHA:w.ArrayRuntime},resources,nil
}
