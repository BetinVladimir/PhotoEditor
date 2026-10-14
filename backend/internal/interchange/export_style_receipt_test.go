package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestExportStyleReceiptExactPolicyAndRuntime(t *testing.T){
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))},Contract:"StyleV2"}
 policy,runtime:=rawSHA([]byte("policy")),rawSHA([]byte("runtime"))
 for _,mode:=range []string{"valid","sha","policy","runtime","size","contract","unknown","canonical"}{t.Run(mode,func(t *testing.T){
  receipt:=ExportNativeStyleReceipt{Schema:1,Contract:"StyleV2",SHA:entry.Ref.SHA256,Bytes:100,Policy:policy,Runtime:runtime}
  switch mode{case "sha":receipt.SHA=policy;case "policy":receipt.Policy=runtime;case "runtime":receipt.Runtime=policy;case "size":receipt.Bytes++;case "contract":receipt.Contract="Params"}
  raw,err:=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)}
  if mode=="unknown"{raw=append([]byte(`{"approved":true,`),raw[1:]...)};if mode=="canonical"{raw=append(raw,'\n')}
  _,err=decodeExportNativeStyleReceipt(raw,entry,100,policy,runtime)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected style receipt: %v",err)}
 })}
}
