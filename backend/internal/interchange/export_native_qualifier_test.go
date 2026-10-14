package interchange
import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type exportNativeProbeStub struct{calls int;runtime,path,mode string;err error}
func(s *exportNativeProbeStub)RunExportNativeProbe(_ context.Context,raw []byte)([]byte,error){
 s.calls++;var req struct{Path string `json:"file_path"`;SHA string `json:"sha256"`;Bytes int64 `json:"bytes"`;Media string `json:"media_type"`};if err:=json.Unmarshal(raw,&req);err!=nil{return nil,err};s.path=req.Path
 if _,err:=os.Stat(req.Path);err!=nil{return nil,err};if s.err!=nil{return nil,s.err}
 if s.mode=="malformed"{return []byte("{}"),nil}
 runtime:=s.runtime;if s.mode=="runtime"{runtime=strings.Repeat("f",64)}
 return canonical.Marshal(map[string]any{"schema_version":1,"sha256":req.SHA,"bytes":req.Bytes,"media_type":req.Media,"width":10,"height":10,"bit_depth":8,"has_alpha":false,"orientation":1,"icc_sha256":nil,"runtime_sha256":runtime})
}
func TestNativeExportImageQualifierStagesBoundsAndCleans(t *testing.T){
 for _,mode:=range []string{"valid","hash","budget","runtime","probe_error","malformed","role","cancelled"}{t.Run(mode,func(t *testing.T){
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,100);if err!=nil{t.Fatal(err)}
  body:=[]byte("image");entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Contract:"EncodedImage"};object:=objectstore.Object{Key:"image",VersionID:"v1",SHA256:entry.Ref.SHA256,Bytes:5,ContentType:"image/tiff"}
  objects:=&importReaderStub{body:body};runtime:=rawSHA([]byte("runtime"));cause:=errors.New("native decode refused");probe:=&exportNativeProbeStub{runtime:runtime,mode:mode};q:=NativeExportImageQualifier{Objects:objects,Scratch:scratch,Probe:probe,RuntimeSHA:runtime};ctx:=context.Background()
  switch mode{case "hash":objects.body=[]byte("wrong");case "budget":scratch.maximum=5;case "probe_error":probe.err=cause;case "role":entry.Contract="MaskArray";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  err=q.QualifyExportBinary(ctx,entry,object)
  if mode=="valid"{if err!=nil||probe.calls!=1||!strings.HasSuffix(probe.path,".tiff"){t.Fatal("valid bridge",err)}}else if err==nil{t.Fatal("invalid native admission accepted",mode)}
  if mode=="probe_error"&&!errors.Is(err,cause){t.Fatal("native cause lost")}
  if (mode=="hash"||mode=="budget"||mode=="role"||mode=="cancelled")&&probe.calls!=0{t.Fatal("failed staging reached probe")}
  files,readErr:=os.ReadDir(root);if readErr!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",readErr,scratch.used)}
 })}
}
func TestPythonExportProbePreflightDoesNotLaunchProcess(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(PythonExportNativeProbe{}).RunExportNativeProbe(ctx,[]byte("{}"));err!=context.Canceled{t.Fatal("cancelled process",err)}
 if _,err:=(PythonExportNativeProbe{}).RunExportNativeProbe(context.Background(),[]byte("{}"));err!=v2input.ErrInvalid{t.Fatal("invalid process config",err)}
}
