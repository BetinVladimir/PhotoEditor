package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
// Mock receipts cover orchestration, not production mask schemas.
func TestQualifiedAlignmentBackgroundRequiresCurrentPolicyAndNativeProof(t *testing.T){
 for _,mode:=range []string{"valid","policy_changed","probe","backend"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  alignment:=v2input.Entry{Contract:"PairAlignment",Ref:ref()};after:=ref()
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":after,"alignment":alignment.Ref,"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  params,err:=canonical.Marshal(map[string]any{"before":image.Entry.Ref,"after":after,"background_mask":mask});if err!=nil{t.Fatal(err)}
  registration:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:"pair.register",Version:"1"},Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)}}
  root,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  doc:=func(key string,body []byte)ExportSeriesMaskDocument{return ExportSeriesMaskDocument{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Object:objectstore.Object{Key:key,VersionID:"immutable",SHA256:rawSHA(body),Bytes:int64(len(body)),ContentType:"application/json"}}}
  report:=[]byte(`{"backend":"cpu"}`);if mode=="backend"{report=[]byte(`{"backend":"mps"}`)}
  source:=&alignmentBackgroundOriginStub{origin:ExportSeriesMaskOrigin{Task:uuid.New(),Operation:"mask.person",Version:"1",Attempt:1,Fence:1,Root:doc("root",root),Params:doc("params",[]byte(`{}`)),Report:doc("report",report),Mask:objectstore.Object{SHA256:mask.Artifact.SHA256}}}
  canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{t.Fatal(err)}
  policy:=&seriesMaskPolicyStub{evidence:ExportSeriesMaskPolicyEvidence{Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:image.Entry.Ref,CanvasHash:canvas},ModelID:"lraspp_mobilenet_v3_large",ModelLock:rawSHA([]byte("model")),Backend:"cpu",QualificationSHA:rawSHA([]byte("qualification"))},change:mode=="policy_changed"}
  rootDir:=t.TempDir();scratch,err:=NewExportArchiveScratch(rootDir,1<<20);if err!=nil{t.Fatal(err)}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  q:=QualifiedExportAlignmentBackgroundSource{Reader:ExportAlignmentBackgroundOriginReader{Source:source,Objects:seriesObjectsStub{bodies:map[string][]byte{"root":root,"params":[]byte(`{}`),"report":report}}},Policy:policy,Scratch:scratch,Probe:probe,RuntimeSHA:runtime}
  object,err:=q.LookupExportAlignmentBackground(context.Background(),registration,parent,raw,fit,alignment,params,image)
  if mode=="valid"{if err!=nil||object!=source.origin.Mask||policy.calls!=2||probe.calls!=1||source.calls!=5{t.Fatal("qualified source incomplete",err)}}else if err==nil||object!=(objectstore.Object{}){t.Fatal("unqualified background returned",err)}
  files,err:=os.ReadDir(rootDir);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
  if rawSHA(params)!=registration.Params.SHA256||rawSHA(image.Body)!=image.Entry.Ref.SHA256||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback mutated caller")}
 })}
}
