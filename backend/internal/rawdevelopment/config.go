package rawdevelopment

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "os"
 "path/filepath"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2qualification"
)

// DeploymentConfig is operator-controlled. DecoderVersion must describe the
// native installation covered by Locks.RuntimeLockSHA; never infer it from RAW.
type DeploymentConfig struct {
 SchemaVersion int `json:"schema_version"`
 ReportID uuid.UUID `json:"report_id"`
 ReportSHA string `json:"report_sha"`
 DecoderVersion string `json:"decoder_version"`
 Locks v2qualification.DeploymentLocks `json:"locks"`
}
func DecodeSourceConfig(ctx context.Context,raw []byte)(Source,error){
 var zero Source
 if err:=ctx.Err();err!=nil{return zero,err}
 if len(raw)<2||len(raw)>1<<20{return zero,v2qualification.ErrUnqualified}
 normalized,err:=canonical.Bytes(raw);if err!=nil{return zero,v2qualification.ErrUnqualified}
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 var config DeploymentConfig
 if decoder.Decode(&config)!=nil||decoder.Decode(new(any))!=io.EOF{return zero,v2qualification.ErrUnqualified}
 encoded,err:=canonical.Marshal(config)
 if err!=nil||!bytes.Equal(normalized,encoded){return zero,v2qualification.ErrUnqualified}
 if config.SchemaVersion!=1||config.ReportID==uuid.Nil{return zero,v2qualification.ErrUnqualified}
 if _,err=domain.ParseDigest(config.ReportSHA);err!=nil{return zero,v2qualification.ErrUnqualified}
 expected,err:=config.Locks.Expected();if err!=nil{return zero,err}
 probe:=&Policy{1,"ahd","camera",0,"clip",config.DecoderVersion}
 if !Valid(probe)||expected.Operation!="asset.decode"||expected.Version!="1"||expected.Backend!="cpu"{return zero,v2qualification.ErrUnqualified}
 return Source{ReportID:config.ReportID,ReportSHA:config.ReportSHA,DecoderVersion:config.DecoderVersion,Expected:expected},nil
}
func LoadSourceConfig(ctx context.Context,path string)(Source,error){
 var zero Source
 if err:=ctx.Err();err!=nil{return zero,err}
 if !filepath.IsAbs(path){return zero,v2qualification.ErrUnqualified}
 before,err:=os.Lstat(path);if err!=nil{return zero,err}
 if !before.Mode().IsRegular()||before.Mode().Perm()&022!=0||before.Size()<2||before.Size()>1<<20{return zero,v2qualification.ErrUnqualified}
 file,err:=os.Open(path);if err!=nil{return zero,err}
 opened,err:=file.Stat();if err!=nil{_ = file.Close();return zero,err}
 if !os.SameFile(before,opened)||opened.Size()!=before.Size()||opened.Mode()!=before.Mode(){_ = file.Close();return zero,v2qualification.ErrUnqualified}
 raw,readErr:=io.ReadAll(io.LimitReader(file,before.Size()+1))
 final,statErr:=file.Stat();closeErr:=file.Close()
 if readErr!=nil{return zero,readErr};if statErr!=nil{return zero,statErr};if closeErr!=nil{return zero,closeErr}
 current,err:=os.Lstat(path);if err!=nil{return zero,err}
 if int64(len(raw))!=before.Size()||!os.SameFile(before,current)||final.Size()!=before.Size()||current.Size()!=before.Size()||final.Mode()!=before.Mode()||current.Mode()!=before.Mode()||!final.ModTime().Equal(before.ModTime())||!current.ModTime().Equal(before.ModTime()){return zero,v2qualification.ErrUnqualified}
 return DecodeSourceConfig(ctx,raw)
}
