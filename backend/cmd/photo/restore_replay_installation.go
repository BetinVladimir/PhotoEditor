package main

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "reflect"
 "syscall"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/queue"
)

type restoreReplayProcessConfig struct {
 Schema string `json:"schema_version"`
 Socket string `json:"socket_path"`
 Token string `json:"socket_token"`
 Database string `json:"database"`
 Vhost string `json:"vhost"`
 Maximum int64 `json:"maximum_events"`
 Root string `json:"receipt_root"`
 RunID uuid.UUID `json:"run_id"`
 RecoveryPath string `json:"recovery_report_path"`
 RecoverySHA string `json:"recovery_sha256"`
 BuildSHA string `json:"build_sha256"`
}

// Both protocol objects are flat and closed. encoding/json otherwise accepts
// repeated keys, which must not turn a contradictory refusal into authority.
func decodeRestoreReplayObject(raw []byte, target any) error {
 targetType:=reflect.TypeOf(target)
 if targetType==nil||targetType.Kind()!=reflect.Pointer||targetType.Elem().Kind()!=reflect.Struct{return errors.New("restore replay JSON target invalid")}
 fields:=map[string]bool{}
 for index:=0;index<targetType.Elem().NumField();index++{fields[targetType.Elem().Field(index).Tag.Get("json")]=true}
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 first,err:=decoder.Token();if err!=nil||first!=json.Delim('{'){return errors.New("restore replay JSON object invalid")}
 seen:=map[string]bool{}
 for decoder.More(){
  token,err:=decoder.Token();if err!=nil{return err};name,ok:=token.(string)
  if !ok||!fields[name]||seen[name]{return errors.New("restore replay JSON unknown or duplicate field")};seen[name]=true
  var value json.RawMessage;if err=decoder.Decode(&value);err!=nil{return err}
 }
 last,err:=decoder.Token();if err!=nil||last!=json.Delim('}'){return errors.New("restore replay JSON object invalid")}
 if len(seen)!=len(fields){return errors.New("restore replay JSON required field missing")}
 var trailing any;if decoder.Decode(&trailing)!=io.EOF{return errors.New("restore replay JSON trailing value")}
 decoder=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 return decoder.Decode(target)
}

func readRestoreReplayProcessConfig(path,pin string)(restoreReplayProcessConfig,error){
 var c restoreReplayProcessConfig
 refuse:=errors.New("restore replay process installation invalid")
 digest:=regexp.MustCompile(`^[0-9a-f]{64}$`)
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path||!digest.MatchString(pin){return c,refuse}
 resolved,err:=filepath.EvalSymlinks(path);if err!=nil||resolved!=path{return c,refuse}
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK,0);if err!=nil{return c,refuse};defer file.Close()
 before,err:=file.Stat();if err!=nil||!before.Mode().IsRegular()||before.Mode().Perm()&0077!=0||before.Size()<2||before.Size()>16384{return c,refuse}
 owner,ok:=before.Sys().(*syscall.Stat_t);if !ok||owner.Uid!=uint32(os.Getuid())||owner.Nlink!=1{return c,refuse}
 raw,err:=io.ReadAll(io.LimitReader(file,16385));if err!=nil||int64(len(raw))!=before.Size(){return c,refuse}
 after,err:=os.Lstat(path);if err!=nil||!os.SameFile(before,after)||before.Size()!=after.Size()||before.ModTime()!=after.ModTime()||!after.Mode().IsRegular()||after.Mode().Perm()&0077!=0{return c,refuse}
 afterOwner,ok:=after.Sys().(*syscall.Stat_t);if !ok||afterOwner.Uid!=uint32(os.Getuid())||afterOwner.Nlink!=1{return c,refuse}
 resolved,err=filepath.EvalSymlinks(path);if err!=nil||resolved!=path{return c,refuse}
 sum:=sha256.Sum256(raw);if hex.EncodeToString(sum[:])!=pin{return c,refuse}
 if decodeRestoreReplayObject(raw,&c)!=nil{return c,refuse}
 if c.Schema!="restore-replay-process/v1"||c.RunID==uuid.Nil||!digest.MatchString(c.Token)||!digest.MatchString(c.RecoverySHA)||!digest.MatchString(c.BuildSHA)||c.Database==""||len(c.Database)>63||c.Vhost==""||c.Vhost=="/"||c.Maximum<1||c.Maximum>10000000{return c,refuse}
 for _,p:=range []string{c.Socket,c.Root,c.RecoveryPath}{if !filepath.IsAbs(p)||filepath.Clean(p)!=p{return c,refuse}}
 if filepath.Base(c.RecoveryPath)!=c.RunID.String()+".json"{return c,refuse}
 return c,nil
}

// The operator owns this private live socket. Every invocation rechecks actual
// Compose/network/database isolation; a pinned config alone grants no authority.
func restoreReplaySocketGuard(c restoreReplayProcessConfig)(func(context.Context)error,func()){
 transport:=&http.Transport{DisableKeepAlives:true}
 transport.DialContext=func(ctx context.Context,_,_ string)(net.Conn,error){
  refuse:=errors.New("restore replay live authority unavailable")
  for _,path:=range []string{filepath.Dir(c.Socket),c.Socket}{
   resolved,err:=filepath.EvalSymlinks(path);if err!=nil||resolved!=path{return nil,refuse}
   info,err:=os.Lstat(path);if err!=nil{return nil,refuse}
   if path==c.Socket {if info.Mode().Perm()!=0600{return nil,refuse}} else if !info.IsDir()||info.Mode().Perm()!=0700{return nil,refuse}
   owner,ok:=info.Sys().(*syscall.Stat_t);if !ok||owner.Uid!=uint32(os.Getuid()){return nil,refuse}
   if path==c.Socket&&info.Mode()&os.ModeSocket==0{return nil,refuse}
  }
  return (&net.Dialer{Timeout:5*time.Second}).DialContext(ctx,"unix",c.Socket)
 }
 client:=&http.Client{Transport:transport,Timeout:10*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
 guard:=func(ctx context.Context)error{
  if err:=ctx.Err();err!=nil{return err}
  requestBody,_:=json.Marshal(map[string]string{"token":c.Token,"run_id":c.RunID.String(),"database":c.Database})
  request,err:=http.NewRequestWithContext(ctx,http.MethodPost,"http://restore-authority/check",bytes.NewReader(requestBody));if err!=nil{return err}
  request.Close=true;request.Header.Set("Content-Type","application/json")
  response,err:=client.Do(request);if err!=nil{if ctx.Err()!=nil{return ctx.Err()};return errors.New("restore replay live authority unavailable")};defer response.Body.Close()
  raw,err:=io.ReadAll(io.LimitReader(response.Body,1025));if ctx.Err()!=nil{return ctx.Err()};if err!=nil||len(raw)>1024||response.StatusCode!=200{return errors.New("restore replay isolation refused")}
  var verdict struct{Authorized bool `json:"authorized"`;Token string `json:"token"`}
  if decodeRestoreReplayObject(raw,&verdict)!=nil||!verdict.Authorized||verdict.Token!=c.Token{return errors.New("restore replay isolation refused")}
  return ctx.Err()
 }
 return guard,transport.CloseIdleConnections
}

func installRestoreReplayProcess(ctx context.Context,path,pin string,deps apiProcessDependencies)(apiProcessDependencies,func(),error){
 close:=func(){}
 if err:=ctx.Err();err!=nil{return deps,close,err}
 if deps.RestoreReplay!=nil&&deps.RestoreReplayReceipt!=nil&&path==""&&pin==""{return deps,close,nil}
 if deps.RestoreReplay!=nil||deps.RestoreReplayReceipt!=nil{return deps,close,errors.New("restore replay installation conflict")}
 c,err:=readRestoreReplayProcessConfig(path,pin);if err!=nil{return deps,close,err}
 guard,cleanup:=restoreReplaySocketGuard(c)
 if err=guard(ctx);err!=nil{cleanup();return deps,close,err}
 if err=ctx.Err();err!=nil{cleanup();return deps,close,err}
 deps.RestoreReplay=&queue.RestoreReplayInstallation{Database:c.Database,Vhost:c.Vhost,MaximumEvents:c.Maximum,AssertIsolation:guard}
 deps.RestoreReplayReceipt=&queue.RestoreReplayReceiptInstallation{Root:c.Root,RunID:c.RunID,RecoveryReportPath:c.RecoveryPath,RecoverySHA256:c.RecoverySHA,BuildSHA256:c.BuildSHA}
 return deps,cleanup,nil
}
