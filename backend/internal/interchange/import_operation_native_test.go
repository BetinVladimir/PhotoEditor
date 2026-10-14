package interchange
import (
 "context"
 "errors"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type nativeOperationPolicyStub struct{calls,failAt int;failure error}
func(p *nativeOperationPolicyStub)QualifyImportedLayerOperation(_ context.Context,_ string,_ string,raw []byte,_ v2input.SessionVersionBinding)error{
 p.calls++;if string(raw)!="{}"{return errors.New("aliased Params")};raw[0]='!';if p.calls==p.failAt{return p.failure};return nil
}
// Native receipt mocked: full production Params schemas are not qualified here.
func TestNativeImportOperationSchemaKeepsIndependentPolicy(t *testing.T){
 for _,mode:=range []string{"valid","policy","revoked","probe","unknown","cancelled"}{t.Run(mode,func(t *testing.T){
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20};runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime}
  sentinel:=errors.New("runtime revoked");policy:=&nativeOperationPolicyStub{failure:sentinel}
  base,err:=NewImportOperationRouter(context.Background(),map[ImportOperationVersion]ImportLayerOperationQualifier{{"portrait.hair","1"}:policy});if err!=nil{t.Fatal(err)}
  installed,err:=(ImportNativeOperationInstallation{Scratch:scratch,Probe:probe,Models:map[string]string{},RuntimeSHA:runtime}).Build(context.Background(),base);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))},CanvasHash:rawSHA([]byte("canvas"))}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();operation:="portrait.hair"
  switch mode{case "policy":policy.failAt=1;case "revoked":policy.failAt=2;case "probe":probe.err=sentinel;case "unknown":operation="portrait.unknown";case "cancelled":cancel()}
  raw:=[]byte(`{}`);err=installed.QualifyImportedLayerOperation(ctx,operation,"1",raw,binding)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected native operation admission",err)}
  if mode=="valid"&&(policy.calls!=2||probe.calls!=1||string(raw)!="{}"){t.Fatal("policy/schema/ownership sequence")}
  if mode=="policy"&&probe.calls!=0{t.Fatal("refusal reached native IO")}
  if mode=="unknown"||mode=="cancelled"{if policy.calls!=0||probe.calls!=0{t.Fatal("unknown/cancelled operation performed IO")}}
  if mode=="policy"||mode=="revoked"||mode=="probe"{if !errors.Is(err,sentinel){t.Fatal("refusal lost",err)}}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("native Params scratch leaked",err)}
  if _,wrapped:=base.operations[ImportOperationVersion{"portrait.hair","1"}].(nativeImportOperationQualifier);wrapped{t.Fatal("base registry mutated")}
 })}
}
func TestNativeDenoiseOperationIsExplicitlySupported(t *testing.T){
 if !nativeImportOperationSupported(ImportOperationVersion{"image.denoise","1"}){t.Fatal("denoise schema missing")}
 if nativeImportOperationSupported(ImportOperationVersion{"image.denoise","2"}){t.Fatal("uninstalled denoise version")}
 operation,err:=exportParamsOperationShape([]byte(`{"model_id":"scunet_color_real_psnr"}`))
 if err!=nil||operation!="image.denoise"{t.Fatal("original Params operation proof omitted",err)}
}
