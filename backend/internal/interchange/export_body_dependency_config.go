package interchange

import (
 "context"
 "io"
 "os"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// Operator-only complete policy inventory. No partial set or inferred model
// identifiers; reports must independently qualify each operation.
type ExportBodyDependencyPolicyFile struct{
 Schema int `json:"schema_version"`
 Policies []ExportBodyDependencyPolicyFileEntry `json:"policies"`
}
type ExportBodyDependencyPolicyFileEntry struct{
 Report v2input.Ref `json:"qualification_report"`
 Locks v2qualification.DeploymentLocks `json:"locks"`
}
func DecodeExportBodyDependencyPolicyConfig(ctx context.Context,raw []byte)([]ExportBodyDependencyPolicyConfig,error){
 if err:=ctx.Err();err!=nil{return nil,err};if len(raw)<2||len(raw)>1<<20{return nil,v2input.ErrInvalid}
 var file ExportBodyDependencyPolicyFile
 if err:=decodeImportDocument(raw,&file);err!=nil{return nil,err}
 if file.Schema!=1||len(file.Policies)!=3{return nil,v2input.ErrInvalid}
 seen:=map[string]bool{};reports:=map[[16]byte]bool{};configs:=[]ExportBodyDependencyPolicyConfig{}
 for _,entry:=range file.Policies{
  operation:=entry.Locks.Operation
  if !exportBodyDependencyQualificationOperation(operation)||seen[operation]||entry.Locks.Version!="1"||!validImportRef(entry.Report)||reports[entry.Report.ArtifactID]{return nil,v2input.ErrInvalid}
  seen[operation]=true;reports[entry.Report.ArtifactID]=true
  expected,err:=entry.Locks.Expected();if err!=nil{return nil,err}
  if operation=="pair.fit.body"&&expected.Backend!="cpu"{return nil,v2input.ErrInvalid}
  configs=append(configs,ExportBodyDependencyPolicyConfig{Report:entry.Report,Expected:expected})
 }
 if err:=ctx.Err();err!=nil{return nil,err};return configs,nil
}
func LoadExportBodyDependencyPolicyConfig(ctx context.Context,path string)([]ExportBodyDependencyPolicyConfig,error){
 var zero []ExportBodyDependencyPolicyConfig
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
 return DecodeExportBodyDependencyPolicyConfig(ctx,raw)
}
