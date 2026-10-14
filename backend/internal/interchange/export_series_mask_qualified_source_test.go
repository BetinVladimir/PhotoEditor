package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type seriesMaskPolicyStub struct{evidence ExportSeriesMaskPolicyEvidence;calls int;change bool}
func(s *seriesMaskPolicyStub)ResolveExportSeriesMaskPolicy(context.Context,ExportSeriesMaskOrigin,v2input.Ref)(ExportSeriesMaskPolicyEvidence,error){s.calls++;out:=s.evidence;if s.change&&s.calls>1{out.QualificationSHA=rawSHA([]byte("changed qualification"))};return out,nil}
// Receipt stub covers orchestration; production schema tests cover claims.
func TestQualifiedSeriesMaskSourceRequiresStableCurrentPolicy(t *testing.T){
 for _,mode:=range []string{"valid","policy_changed","probe","backend"}{t.Run(mode,func(t *testing.T){
  _,entry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  lock:=rawSHA([]byte("model"));mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("mask"))},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1,ModelLockID:&lock}
  raw,err:=canonical.Marshal(map[string]any{"image":entry.Ref,"regions":[]any{map[string]any{"mask":mask}}});if err!=nil{t.Fatal(err)}
  measure:=ExportSeriesMeasureOrigin{Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  root,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  report:=[]byte(`{"backend":"cpu"}`);if mode=="backend"{report=[]byte(`{"backend":"mps"}`)}
  doc:=func(key string,body []byte)ExportSeriesMaskDocument{return ExportSeriesMaskDocument{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Object:objectstore.Object{Key:key,VersionID:"immutable",SHA256:rawSHA(body),Bytes:int64(len(body)),ContentType:"application/json"}}}
  source:=&seriesMaskOriginStub{origin:ExportSeriesMaskOrigin{Task:uuid.New(),Operation:"mask.person",Version:"1",Attempt:1,Fence:1,Root:doc("root",root),Params:doc("params",[]byte(`{}`)),Report:doc("report",report),Mask:objectstore.Object{SHA256:mask.Artifact.SHA256}}}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  policy:=&seriesMaskPolicyStub{evidence:ExportSeriesMaskPolicyEvidence{Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas},ModelID:"lraspp_mobilenet_v3_large",ModelLock:lock,Backend:"cpu",QualificationSHA:rawSHA([]byte("qualification"))},change:mode=="policy_changed"}
  rootDir:=t.TempDir();scratch,err:=NewExportArchiveScratch(rootDir,1<<20);if err!=nil{t.Fatal(err)}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  q:=QualifiedExportSeriesMeasureMaskSource{Reader:ExportSeriesMaskOriginReader{Source:source,Objects:seriesObjectsStub{bodies:map[string][]byte{"root":root,"params":[]byte(`{}`),"report":report}}},Policy:policy,Scratch:scratch,Probe:probe,RuntimeSHA:runtime}
  object,err:=q.LookupExportSeriesMeasureMask(context.Background(),measure,v2input.Entry{},raw,ExportClosureNode{Entry:entry,Body:imageRaw},v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256})
  if mode=="valid"{if err!=nil||object!=source.origin.Mask||policy.calls!=2||probe.calls!=1{t.Fatal(err)}}else if err==nil||object!=(objectstore.Object{}){t.Fatal("unqualified source returned",mode,err)}
  files,err:=os.ReadDir(rootDir);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
 })}
}
