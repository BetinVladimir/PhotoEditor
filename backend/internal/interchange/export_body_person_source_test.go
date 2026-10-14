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
type bodyPersonOriginStub struct{calls int}
func(s *bodyPersonOriginStub)LookupExportBodyPersonOrigin(context.Context,v2input.Entry,[]byte,ExportClosureNode)(ExportStatsPersonOrigin,error){s.calls++;return ExportStatsPersonOrigin{},nil}
type bodyPersonSequence struct{object objectstore.Object;calls int;mode string;paramsSHA,imageSHA string;t *testing.T}
func(s *bodyPersonSequence)LookupExportBodyPerson(_ context.Context,_ v2input.Entry,raw []byte,image ExportClosureNode)(objectstore.Object,error){
 s.calls++;if rawSHA(raw)!=s.paramsSHA||rawSHA(image.Body)!=s.imageSHA{s.t.Fatal("source evidence mutated")};raw[0]='!';image.Body[0]='!'
 out:=s.object;if s.calls>1{if s.mode=="changed"{out.VersionID="changed"};if s.mode=="revoked"{return objectstore.Object{},v2input.ErrInvalid}};return out,nil
}
func bodyPersonFixture(t *testing.T)(v2input.Entry,[]byte,ExportClosureNode,review.MaskRef){
 t.Helper();_,source,rawImage:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(rawImage);if err!=nil{t.Fatal(err)}
 mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("mask"))},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}
 raw,err:=canonical.Marshal(map[string]any{"image":source.Ref,"poses":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("poses"))},"person":mask,"coefficients":map[string]any{"face_slim":0,"waist_slim":0,"hip_slim":0,"torso_slim":0,"leg_length":0}});if err!=nil{t.Fatal(err)}
 return v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}},raw,ExportClosureNode{Entry:source,Body:rawImage},mask
}
func TestBodyPersonSourceRequiresCurrentPolicyBeforeDocumentIO(t *testing.T){
 parent,raw,image,_:=bodyPersonFixture(t)
 origin:=&bodyPersonOriginStub{};policy:=&statsPersonPolicyDenied{}
 source:=QualifiedExportBodyPersonSource{Origin:origin,Policy:policy,Documents:ExportStatsPersonDocumentReader{Catalog:&statsPersonDocumentCatalogStub{},Objects:exportDefectNoIO{t}}}
 if result,err:=source.LookupExportBodyPerson(context.Background(),parent,raw,image);err==nil||result!=(objectstore.Object{})||origin.calls!=1||policy.calls!=1{t.Fatal("current person policy bypassed",err)}
}
// Native mock proves streaming/authority orchestration, not mask array content.
func TestBodyPersonPhysicalQualifierRechecksAndOwnsEvidence(t *testing.T){
 for _,mode:=range []string{"valid","changed","revoked","probe","bytes"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  source:=&bodyPersonSequence{t:t,mode:mode,paramsSHA:parent.Ref.SHA256,imageSHA:image.Entry.Ref.SHA256,object:objectstore.Object{Key:"person",VersionID:"immutable",SHA256:mask.Artifact.SHA256,Bytes:4,ContentType:"application/x-npy"}}
  bytes:=[]byte("mask");if mode=="bytes"{bytes=[]byte("oops")}
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1000};runtime:=rawSHA([]byte("runtime"));probe:=&exportMaskProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  qualifier:=NativeExportBodyPersonQualifier{Source:source,Objects:statsParamsObjects{bytes},Scratch:scratch,Probe:probe,RuntimeSHA:runtime}
  err:=qualifier.QualifyExportBodyPerson(context.Background(),parent,raw,image)
  if (err==nil)!=(mode=="valid"){t.Fatal("unqualified physical person mask",err)}
  expected:=2;if mode=="probe"||mode=="bytes"{expected=1};if source.calls!=expected{t.Fatal("source not rechecked")}
  if rawSHA(raw)!=parent.Ref.SHA256||rawSHA(image.Body)!=image.Entry.Ref.SHA256{t.Fatal("caller evidence mutated")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("mask scratch leaked",err)}
 })}
}
