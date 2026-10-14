package interchange
import (
 "context"
 "errors"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type nativeImportOverridePolicy struct{calls int;failAt int;failure error;binding v2input.SessionVersionBinding}
func(p *nativeImportOverridePolicy)QualifyImportOverrideContent(_ context.Context,raw []byte,binding v2input.SessionVersionBinding)error{
 p.calls++;p.binding=binding
 if string(raw)!="{}"{return errors.New("policy buffer was mutated")};raw[0]='!'
 if p.calls==p.failAt{return p.failure};return nil
}
// Probe is mocked: these prove composition and cleanup, not native module validity.
func TestNativeImportOverrideRequiresSchemaAndRepeatedCurrentPolicy(t *testing.T){
 for _,mode:=range []string{"valid","policy","revoked","probe","binding","cancelled","noncanonical"}{t.Run(mode,func(t *testing.T){
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};sentinel:=errors.New("policy revoked")
  policy:=&nativeImportOverridePolicy{failure:sentinel}
  q,err:=NewNativeImportOverrideContent(scratch,probe,map[string]string{},runtime,policy);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))},CanvasHash:rawSHA([]byte("canvas"))}
  raw:=[]byte(`{}`);ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "policy":policy.failAt=1;case "revoked":policy.failAt=2;case "probe":probe.err=sentinel;case "binding":binding.AssetVersionID=uuid.Nil;case "cancelled":cancel();case "noncanonical":raw=[]byte(`{ }`)}
  err=q.QualifyImportOverrideContent(ctx,raw,binding)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected native override admission",err)}
  if mode=="valid"&&(policy.calls!=2||probe.calls!=1||policy.binding!=binding||string(raw)!="{}"){t.Fatal("native/policy sequence or ownership")}
  if mode=="policy"&&probe.calls!=0{t.Fatal("denied policy reached native IO")}
  if (mode=="policy"||mode=="revoked"||mode=="probe")&&!errors.Is(err,sentinel){t.Fatal("refusal lost",err)}
  if mode=="binding"||mode=="cancelled"{if probe.calls!=0||policy.calls!=0{t.Fatal("invalid request performed IO")}}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("native override scratch leaked",err)}
 })}
}
