package interchange
import (
 "context"
 "testing"
 "os"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type exportParamsOperationStub struct{evidence ExportParamsOperationEvidence;calls int;change bool}
func(s *exportParamsOperationStub)LookupExportParamsOperation(context.Context,v2input.Entry,[]byte)(ExportParamsOperationEvidence,error){s.calls++;out:=s.evidence;if s.change&&s.calls>1{out.Fence++};return out,nil}
// Mock receipt tests staging/order/final proof; Python tests cover schemas.
func TestOperationParamsNativeValidatorRequiresStableProducer(t *testing.T){
 for _,mode:=range []string{"valid","operation","changed","probe"}{t.Run(mode,func(t *testing.T){
  raw:=[]byte(`{"person":{}}`);entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Params"}
  source:=&exportParamsOperationStub{evidence:ExportParamsOperationEvidence{Task:uuid.New(),Operation:"color.stats",Version:"1",Attempt:1,Fence:1},change:mode=="changed"};if mode=="operation"{source.evidence.Operation="portrait.detail"}
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,10000);if err!=nil{t.Fatal(err)};runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  other:=&resolvedOriginOtherSpy{};validator,err:=NewExportOperationParamsValidator(other,source,scratch,probe,map[string]string{},runtime);if err!=nil{t.Fatal(err)}
  children,err:=validator.ExportDocumentChildren(context.Background(),entry,raw)
  if mode=="valid"{if err!=nil||len(children)!=1||source.calls!=2||other.calls!=1{t.Fatal("operation path",err)}}else if err==nil||children!=nil{t.Fatal("unqualified operation accepted")}
  if (mode=="operation"||mode=="probe")&&other.calls!=0{t.Fatal("invalid native schema reached policy")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
 })}
}

func TestExportParamsOperationShapeSeparatesLocalPortraitAndPersonConsumers(t *testing.T){
 for _,test:=range []struct{raw,operation string;invalid bool}{
  {`{"base":{},"detail":0.5}`,"portrait.hair",false},
  {`{"base":{},"wrinkle_strength":0.5}`,"portrait.clothing",false},
  {`{"base":{},"detail":0.5,"wrinkle_strength":0.5}`,"",true},
  {`{"image":{},"person":{}}`,"color.stats",false},
  {`{"image":{},"person":{},"poses":{}}`,"geometry.body",false},
  {`{"image":{},"person":{},"base_aligned":{},"regions":[],"resolved":{}}`,"image.local",false},
 }{operation,err:=exportParamsOperationShape([]byte(test.raw));if (err!=nil)!=test.invalid||operation!=test.operation{t.Fatal(test.raw,operation,err)}}
}

// Shape admission is deliberately narrower than schema/provenance admission.
// Exact refs, masks and producer fences are verified by the native and policy ports.
func TestExportAutomaticRepairShapeCannotFallThrough(t *testing.T){
 for _,tc:=range []struct{raw,operation string;invalid bool}{
  {`{"policy":"conservative_auto_v1","image":{},"proposal":{},"proposal_report":{},"resolved":{}}`,"defects.auto_repair",false},
  {`{"policy":"conservative_auto_v1","image":{},"proposal":null,"resolved":{},"protected":[]}`,"sensor.dust_auto_repair",false},
  {`{"policy":"other","image":{},"proposal":{},"proposal_report":{},"resolved":{}}`,"",true},
  {`{"policy":"copyright_only","image":{}}`,"",false},
  {`{"policy":{"artifact_id":"x"},"image":{},"manifest":{}}`,"",false},
  {`{"policy":null,"image":{},"proposal":{},"proposal_report":{},"resolved":{}}`,"",true},
  {`{"policy":"conservative_auto_v1","image":{},"proposal":{},"resolved":{}}`,"",true},
  {`{"policy":"conservative_auto_v1","image":{},"proposal":{},"proposal_report":{},"resolved":{},"review":{}}`,"",true},
  {`{"policy":"conservative_auto_v1","image":{},"proposal":{},"proposal_report":{},"resolved":{},"protected":[]}`,"",true},
 }{op,err:=exportParamsOperationShape([]byte(tc.raw));if (err!=nil)!=tc.invalid||op!=tc.operation{t.Fatal(tc.raw,op,err)}}
}
