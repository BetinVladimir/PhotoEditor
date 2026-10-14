package provenance

import (
 "context"
 "os"
 "path/filepath"
 "syscall"
 "testing"
)

func TestPinnedSigningDeploymentFileEnvelope(t *testing.T){
 for _,mode:=range []string{"valid","sha","invalid_pin","writable","symlink","hardlink","fifo","directory","oversize","relative","cancelled"}{t.Run(mode,func(t *testing.T){
  path:=filepath.Join(t.TempDir(),"deployment.json");raw:=[]byte(`{}`)
  if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
  expected:=bodyDigest(raw)
  switch mode{
  case "sha":expected=bodyDigest([]byte(`{"other":true}`))
  case "invalid_pin":expected="invalid"
  case "writable":if err:=os.Chmod(path,0666);err!=nil{t.Fatal(err)}
  case "symlink":link:=path+".link";if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)};path=link
  case "hardlink":if err:=os.Link(path,path+".link");err!=nil{t.Fatal(err)}
  case "fifo":if err:=os.Remove(path);err!=nil{t.Fatal(err)};if err:=syscall.Mkfifo(path,0600);err!=nil{t.Fatal(err)}
  case "directory":path=filepath.Dir(path)
  case "oversize":if err:=os.Truncate(path,(8<<20)+1);err!=nil{t.Fatal(err)}
  case "relative":path="deployment.json"
  }
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  result,err:=readPinnedSigningDeployment(ctx,path,expected)
  if (err==nil)!=(mode=="valid"){t.Fatalf("mode=%s err=%v",mode,err)}
  if mode=="valid"&&string(result)!=string(raw){t.Fatal("file bytes changed")}
 })}
}
func TestSigningDeploymentFileStillRequiresClosedSchema(t *testing.T){
 path:=filepath.Join(t.TempDir(),"deployment.json");raw:=[]byte(`{}`)
 if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
 if _,err:=LoadSigningAPIDeploymentDocument(context.Background(),path,bodyDigest(raw));err==nil{t.Fatal("file pin replaced schema checks")}
}

// Cancel at the final activity boundary, after the pinned bytes were read.
// A failed load must not leak an apparently usable deployment body.
type signingDeploymentFinalCancellation struct {
 context.Context
 calls int
}
func (c *signingDeploymentFinalCancellation) Err() error {
 c.calls++
 if c.calls >= 3 { return context.Canceled }
 return nil
}
func TestPinnedSigningDeploymentFinalCancellationReturnsNoBytes(t *testing.T) {
 path := filepath.Join(t.TempDir(), "deployment.json")
 raw := []byte(`{}`)
 if err := os.WriteFile(path, raw, 0600); err != nil { t.Fatal(err) }
 ctx := &signingDeploymentFinalCancellation{Context: context.Background()}
 result, err := readPinnedSigningDeployment(ctx, path, bodyDigest(raw))
 if err != context.Canceled || result != nil {
  t.Fatalf("cancelled load returned body=%q err=%v", result, err)
 }
 if ctx.calls != 3 { t.Fatalf("unexpected activity boundaries: %d", ctx.calls) }
}
