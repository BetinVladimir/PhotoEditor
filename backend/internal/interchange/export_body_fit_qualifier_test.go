package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type bodyFitBundleStub struct{calls int;change bool}
func(s *bodyFitBundleStub)ReadExportBodyFitInputs(_ context.Context,_ v2input.Entry,raw []byte,fit ExportClosureNode)([]byte,error){
 s.calls++;raw[0]='!';fit.Body[0]='!'
 if s.change&&s.calls>1{return []byte(`{"changed":true}`),nil};return []byte(`{}`),nil
}
type bodyFitCausalStub struct{calls,deny int}
func(s *bodyFitCausalStub)QualifyExportBodyFitCausalInputs(_ context.Context,_ v2input.Entry,raw []byte,fit ExportClosureNode,bundle []byte)error{
 s.calls++;raw[0]='!';fit.Body[0]='!';bundle[0]='!';if s.calls==s.deny{return v2input.ErrInvalid};return nil
}
// Fake native receipts prove mandatory orchestration, not production schemas.
func TestNativeBodyFitQualifierRequiresCurrentCausalAuthority(t *testing.T){
 for _,mode:=range []string{"valid","causal_before","causal_after","changed","probe","cancelled"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":ref(),"alignment":ref(),"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  reader:=&bodyFitBundleStub{change:mode=="changed"};causal:=&bodyFitCausalStub{}
  if mode=="causal_before"{causal.deny=1};if mode=="causal_after"{causal.deny=2}
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20};runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  q,err:=NewNativeExportBodyFitQualifier(reader,causal,scratch,probe,map[string]string{},runtime);if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=q.QualifyExportBodyFitInputs(ctx,parent,raw,fit)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected paired qualification",err)}
  if mode=="valid"&&(reader.calls!=2||causal.calls!=2||probe.calls!=1){t.Fatal("missing final authority")}
  if (mode=="causal_before"||mode=="cancelled")&&probe.calls!=0{t.Fatal("preflight reached native")}
  if rawSHA(raw)!=parent.Ref.SHA256||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback mutated evidence")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
 })}
}
