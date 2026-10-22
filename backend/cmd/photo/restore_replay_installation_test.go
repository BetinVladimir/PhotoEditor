package main

import (
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
 "strings"
 "sync/atomic"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/queue"
)

func replayPrivateDirectory(t *testing.T) string {
 t.Helper();raw,err:=os.MkdirTemp("", "rr-");if err!=nil{t.Fatal(err)}
 path,err:=filepath.EvalSymlinks(raw);if err!=nil{t.Fatal(err)}
 if err=os.Chmod(path,0700);err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ = os.RemoveAll(path)});return path
}
func replayConfig(t *testing.T, root string) restoreReplayProcessConfig {
 t.Helper();run:=uuid.New()
 return restoreReplayProcessConfig{Schema:"restore-replay-process/v1",Socket:filepath.Join(root,"a.sock"),Token:strings.Repeat("a",64),Database:"restored",Vhost:"restore-isolated",Maximum:100,Root:root,RunID:run,RecoveryPath:filepath.Join(root,run.String()+".json"),RecoverySHA:strings.Repeat("b",64),BuildSHA:strings.Repeat("c",64)}
}
func replayConfigFile(t *testing.T, root string, raw []byte)(string,string){
 t.Helper();path:=filepath.Join(root,"process.json")
 if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(raw);return path,hex.EncodeToString(sum[:])
}
func replaySocketServer(t *testing.T, config restoreReplayProcessConfig, handler http.HandlerFunc){
 t.Helper();listener,err:=net.Listen("unix",config.Socket);if err!=nil{t.Fatal(err)}
 if err=os.Chmod(config.Socket,0600);err!=nil{listener.Close();t.Fatal(err)}
 server:=&http.Server{Handler:handler,ReadHeaderTimeout:time.Second}
 go func(){_ = server.Serve(listener)}()
 t.Cleanup(func(){_ = server.Close();_ = listener.Close()})
}
func TestRestoreReplayProcessConfigStrictPinnedPrivateFile(t *testing.T){
 for _,mode:=range []string{"valid","wrong_pin","uppercase_pin","unknown","duplicate","extra_json","array","null","public","symlink","hardlink","relative","bad_token","bad_recovery_pin","wrong_recovery_name","zero_run","root_vhost","zero_maximum"}{t.Run(mode,func(t *testing.T){
  root:=replayPrivateDirectory(t);config:=replayConfig(t,root)
  switch mode {case "bad_token":config.Token=strings.Repeat("A",64);case "bad_recovery_pin":config.RecoverySHA="bad";case "wrong_recovery_name":config.RecoveryPath=filepath.Join(root,"other.json");case "zero_run":config.RunID=uuid.Nil;case "root_vhost":config.Vhost="/";case "zero_maximum":config.Maximum=0}
  raw,err:=json.Marshal(config);if err!=nil{t.Fatal(err)}
  switch mode {case "unknown":raw=append(raw[:len(raw)-1],[]byte(`,"authorized":true}`)...);case "duplicate":raw=append(raw[:len(raw)-1],[]byte(`,"database":"restored"}`)...);case "extra_json":raw=append(raw,[]byte(` {}`)...);case "array":raw=[]byte(`[]`);case "null":raw=[]byte(`null`)}
  path,pin:=replayConfigFile(t,root,raw)
  switch mode {case "wrong_pin":pin=strings.Repeat("0",64);case "uppercase_pin":pin=strings.Repeat("A",64);case "public":err=os.Chmod(path,0644);case "symlink":link:=filepath.Join(root,"link.json");err=os.Symlink(path,link);path=link;case "hardlink":err=os.Link(path,filepath.Join(root,"hard.json"));case "relative":path="process.json"}
  if err!=nil{t.Fatal(err)}
  got,err:=readRestoreReplayProcessConfig(path,pin)
  if mode=="valid"{if err!=nil||got!=config{t.Fatalf("valid installation rejected: %v",err)}}else if err==nil{t.Fatalf("%s accepted",mode)}
 })}
}
func TestRestoreReplaySocketGuardStrictLiveProtocol(t *testing.T){
 for _,mode:=range []string{"valid","denied","wrong_token","unknown","duplicate","case_alias","missing","extra_json","oversized","non_200","redirect"}{t.Run(mode,func(t *testing.T){
  root:=replayPrivateDirectory(t);config:=replayConfig(t,root)
  replaySocketServer(t,config,func(w http.ResponseWriter,r *http.Request){
   if r.Method!=http.MethodPost||r.URL.Path!="/check"||r.Header.Get("Content-Type")!="application/json"{t.Error("incorrect live authority request")}
   raw,err:=io.ReadAll(io.LimitReader(r.Body,1024));if err!=nil{t.Error(err)}
   var body map[string]string;if err=json.Unmarshal(raw,&body);err!=nil{t.Error(err)}
   if len(body)!=3||body["token"]!=config.Token||body["run_id"]!=config.RunID.String()||body["database"]!=config.Database{t.Error("scope not bound to request")}
   response:=`{"authorized":true,"token":"`+config.Token+`"}`
   switch mode {case "denied":response=`{"authorized":false,"token":"`+config.Token+`"}`;case "wrong_token":response=`{"authorized":true,"token":"wrong"}`;case "unknown":response=`{"authorized":true,"token":"`+config.Token+`","extra":true}`;case "duplicate":response=`{"authorized":false,"authorized":true,"token":"`+config.Token+`"}`;case "extra_json":response+=` {}`;case "oversized":response=strings.Repeat(" ",1025);case "non_200":w.WriteHeader(http.StatusForbidden);case "redirect":w.Header().Set("Location","http://external.invalid/check");w.WriteHeader(http.StatusTemporaryRedirect)}
   if mode=="case_alias"{response=`{"authorized":false,"Authorized":true,"token":"`+config.Token+`"}`}
   if mode=="missing"{response=`{"authorized":true}`}
   _,_=io.WriteString(w,response)
  })
  guard,close:=restoreReplaySocketGuard(config);defer close();err:=guard(context.Background())
  if (err==nil)!=(mode=="valid"){t.Fatalf("mode %s err=%v",mode,err)}
 })}
}
func TestRestoreReplaySocketGuardRechecksPrivacyEveryCall(t *testing.T){
 root:=replayPrivateDirectory(t);config:=replayConfig(t,root);var calls atomic.Int32
 replaySocketServer(t,config,func(w http.ResponseWriter,r *http.Request){calls.Add(1);_,_=io.WriteString(w,`{"authorized":true,"token":"`+config.Token+`"}`)})
 guard,close:=restoreReplaySocketGuard(config);defer close()
 if err:=guard(context.Background());err!=nil{t.Fatal(err)}
 if err:=os.Chmod(root,0755);err!=nil{t.Fatal(err)}
 if err:=guard(context.Background());err==nil{t.Fatal("changed parent permissions accepted")}
 if calls.Load()!=1{t.Fatal("authority contacted through nonprivate parent")}
 if err:=os.Chmod(root,0700);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(config.Socket,0666);err!=nil{t.Fatal(err)}
 if err:=guard(context.Background());err==nil{t.Fatal("public socket accepted")}
}
func TestRestoreReplaySocketGuardCancellation(t *testing.T){
 root:=replayPrivateDirectory(t);config:=replayConfig(t,root);entered:=make(chan struct{})
 replaySocketServer(t,config,func(w http.ResponseWriter,r *http.Request){close(entered);<-r.Context().Done()})
 guard,closeGuard:=restoreReplaySocketGuard(config);defer closeGuard()
 ctx,cancel:=context.WithCancel(context.Background());defer cancel();result:=make(chan error,1)
 go func(){result<-guard(ctx)}()
 select{case <-entered:case <-time.After(2*time.Second):t.Fatal("authority request absent")};cancel()
 select{case err:=<-result:if !errors.Is(err,context.Canceled){t.Fatalf("cancel lost: %v",err)};case <-time.After(2*time.Second):t.Fatal("guard did not cancel")}
}
func TestRestoreReplayBootstrapInstalledAndFailureFences(t *testing.T){
 root:=replayPrivateDirectory(t);config:=replayConfig(t,root)
 replaySocketServer(t,config,func(w http.ResponseWriter,r *http.Request){_,_=io.WriteString(w,`{"authorized":true,"token":"`+config.Token+`"}`)})
 raw,_:=json.Marshal(config);path,pin:=replayConfigFile(t,root,raw)
 deps,cleanup,err:=installRestoreReplayProcess(context.Background(),path,pin,apiProcessDependencies{})
 if err!=nil{t.Fatal(err)};defer cleanup()
 if deps.RestoreReplay==nil||deps.RestoreReplayReceipt==nil||deps.RestoreReplay.Database!=config.Database||deps.RestoreReplayReceipt.RunID!=config.RunID{t.Fatal("concrete dependencies not installed")}
 if err=deps.RestoreReplay.AssertIsolation(context.Background());err!=nil{t.Fatal(err)}
 for _,partial:=range []apiProcessDependencies{{RestoreReplay:&queue.RestoreReplayInstallation{}},{RestoreReplayReceipt:&queue.RestoreReplayReceiptInstallation{}}}{if _,close,err:=installRestoreReplayProcess(context.Background(),path,pin,partial);err==nil{close();t.Fatal("partial/conflicting installation accepted")}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,close,err:=installRestoreReplayProcess(ctx,"","",deps);!errors.Is(err,context.Canceled){close();t.Fatal("preinstalled dependencies bypassed cancellation",err)}
 if _,close,err:=installRestoreReplayProcess(context.Background(),"","",apiProcessDependencies{});err==nil{close();t.Fatal("unconfigured ordinary binary accepted")}
}
