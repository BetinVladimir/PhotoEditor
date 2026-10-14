package interchange

import (
 "context"
 "io"
 "os"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// Canonical deployment file, provisioned by the operator alongside the SQL
// qualification policy. This is never part of an import/export API payload.
type ExportDetectorPolicyDeployment struct{Expected v2qualification.Expected;Report v2input.Ref}

type ExportDetectorPolicyConfig struct {
 Schema int `json:"schema_version"`
 Report v2input.Ref `json:"qualification_report"`
 Locks v2qualification.DeploymentLocks `json:"locks"`
}
func DecodeExportDetectorPolicyConfig(ctx context.Context,raw []byte)(ExportDetectorPolicyDeployment,error){
 var zero ExportDetectorPolicyDeployment
 if err:=ctx.Err();err!=nil{return zero,err}
 if len(raw)>1<<20{return zero,v2input.ErrInvalid}
 var config ExportDetectorPolicyConfig
 if err:=decodeImportDocument(raw,&config);err!=nil{return zero,err}
 if config.Schema!=1||!validImportRef(config.Report)||config.Locks.Operation!="face.detect"||config.Locks.Version!="1"{return zero,v2input.ErrInvalid}
 expected,err:=config.Locks.Expected();if err!=nil{return zero,err}
 if !validExportDetectorProfile(expected){return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return ExportDetectorPolicyDeployment{Expected:expected,Report:config.Report},nil
}
func LoadExportDetectorPolicyConfig(ctx context.Context,path string)(ExportDetectorPolicyDeployment,error){
 var zero ExportDetectorPolicyDeployment
 if err:=ctx.Err();err!=nil{return zero,err}
 before,err:=os.Lstat(path);if err!=nil{return zero,err}
 if !before.Mode().IsRegular()||before.Mode().Perm()&022!=0||before.Size()<2||before.Size()>1<<20{return zero,v2input.ErrInvalid}
 file,err:=os.Open(path);if err!=nil{return zero,err}
 after,statErr:=file.Stat();if statErr!=nil{_ = file.Close();return zero,statErr}
 if !os.SameFile(before,after)||after.Size()!=before.Size()||after.Mode().Perm()&022!=0{_ = file.Close();return zero,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,file},(1<<20)+1))
 final,finalErr:=file.Stat();closeErr:=file.Close()
 if readErr!=nil{return zero,readErr};if finalErr!=nil{return zero,finalErr};if closeErr!=nil{return zero,closeErr}
 if int64(len(raw))!=before.Size()||final.Size()!=after.Size()||!final.ModTime().Equal(after.ModTime())||final.Mode()!=after.Mode(){return zero,v2input.ErrInvalid}
 current,err:=os.Lstat(path);if err!=nil{return zero,err}
 if !os.SameFile(current,final)||current.Size()!=final.Size()||!current.ModTime().Equal(final.ModTime())||current.Mode()!=final.Mode(){return zero,v2input.ErrInvalid}
 return DecodeExportDetectorPolicyConfig(ctx,raw)
}
