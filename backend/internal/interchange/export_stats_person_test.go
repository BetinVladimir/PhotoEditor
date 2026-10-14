package interchange
import (
 "context"
 "testing"
 "os"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type statsPersonSourceStub struct{calls int}
func(s *statsPersonSourceStub)LookupExportStatsPerson(context.Context,v2input.Entry,v2input.Ref,[]byte,review.WorkingImage)(objectstore.Object,error){s.calls++;return objectstore.Object{},v2input.ErrInvalid}
func TestStatsPersonRefusesMissingOrRevokedAuthorityBeforeIO(t *testing.T){
 _,_,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 paramsRaw,err:=canonical.Marshal(review.StatsParams{Image:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA(imageRaw)},Person:review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("mask"))},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"revoked","missing","hash","image","runtime","cancelled"}{t.Run(mode,func(t *testing.T){
  source:=&statsPersonSourceStub{};scratch,err:=NewExportArchiveScratch(t.TempDir(),1000);if err!=nil{t.Fatal(err)}
  qualifier:=NativeExportStatsPersonQualifier{Source:source,Objects:exportDefectNoIO{t},Scratch:scratch,Probe:&exportStyleProbeStub{},RuntimeSHA:rawSHA([]byte("runtime"))}
  paramsRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(paramsRaw)};entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("stats"))},Contract:"Stats20"}
  owned:=image;body:=append([]byte(nil),paramsRaw...);ctx:=context.Background()
  switch mode{case "missing":qualifier.Source=nil;case "hash":body=[]byte("{}");case "image":owned.Canvas.Width++;case "runtime":qualifier.RuntimeSHA="invalid";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if err:=qualifier.QualifyExportStatsPerson(ctx,entry,paramsRef,body,review.StatsParams{},owned);err==nil{t.Fatal("unqualified person mask accepted")}
  expected:=0;if mode=="revoked"{expected=1};if source.calls!=expected{t.Fatal("unexpected authority reads",source.calls)}
 })}
}

// The probe is mocked here: this exercises authority ordering, ownership and
// cleanup, not the numerical validity of an NPY mask.
type statsPersonSequence struct {
 t *testing.T
 object objectstore.Object
 paramsSHA string
 imageSHA string
 calls int
 mode string
}
func(s *statsPersonSequence)LookupExportStatsPerson(_ context.Context,_ v2input.Entry,_ v2input.Ref,raw []byte,image review.WorkingImage)(objectstore.Object,error){
 s.calls++
 imageRaw,err:=canonical.Marshal(image);if err!=nil{s.t.Fatal(err)}
 if rawSHA(raw)!=s.paramsSHA||rawSHA(imageRaw)!=s.imageSHA{s.t.Fatal("source received mutated original evidence")}
 // A callback must not be able to poison the evidence used on its next call.
 raw[0]='!';if len(image.TransformChain)>0{image.TransformChain[0]='!'}
 result:=s.object
 if s.calls>1{switch s.mode{case "changed":result.VersionID="replacement";case "revoked":return objectstore.Object{},v2input.ErrInvalid}}
 return result,nil
}
func TestStatsPersonNativePathRechecksSourceAndOwnsEvidence(t *testing.T){
 for _,mode:=range []string{"valid","changed","revoked","probe","bytes"}{t.Run(mode,func(t *testing.T){
  _,_,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  maskBody:=[]byte("mask");maskRef:=review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA(maskBody)}
  paramsRaw,err:=canonical.Marshal(review.StatsParams{Image:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA(imageRaw)},Person:review.MaskRef{Artifact:maskRef,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}});if err!=nil{t.Fatal(err)}
  paramsRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(paramsRaw)}
  source:=&statsPersonSequence{t:t,object:objectstore.Object{Key:"person",VersionID:"immutable",SHA256:maskRef.SHA256,Bytes:int64(len(maskBody)),ContentType:"application/x-npy"},paramsSHA:paramsRef.SHA256,imageSHA:rawSHA(imageRaw),mode:mode}
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,1000);if err!=nil{t.Fatal(err)}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportMaskProbeStub{runtime:runtime}
  if mode=="probe"{probe.err=v2input.ErrInvalid};if mode=="bytes"{maskBody=[]byte("oops")}
  qualifier:=NativeExportStatsPersonQualifier{Source:source,Objects:statsParamsObjects{maskBody},Scratch:scratch,Probe:probe,RuntimeSHA:runtime}
  err=qualifier.QualifyExportStatsPerson(context.Background(),v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("stats"))},Contract:"Stats20"},paramsRef,paramsRaw,review.StatsParams{},image)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected qualification result",mode,err)}
  expected:=2;if mode=="probe"||mode=="bytes"{expected=1};if source.calls!=expected{t.Fatal("source recheck ordering",source.calls)}
  if rawSHA(paramsRaw)!=paramsRef.SHA256{t.Fatal("caller Params changed")}
  after,err:=canonical.Marshal(image);if err!=nil||rawSHA(after)!=rawSHA(imageRaw){t.Fatal("caller image changed",err)}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("native scratch leaked",err)}
 })}
}

func TestStatsPersonOriginLocatorRejectsMalformedEvidenceBeforeDatabase(t *testing.T){
 _,_,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 raw,err:=canonical.Marshal(review.StatsParams{Image:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA(imageRaw)},Person:review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("mask"))},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"contract","params-hash","image","cancelled"}{t.Run(mode,func(t *testing.T){
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("stats"))},Contract:"Stats20"};ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)};owned:=image;ctx:=context.Background()
  switch mode{case "contract":entry.Contract="Params";case "params-hash":ref.SHA256=rawSHA([]byte("foreign"));case "image":owned.Canvas.Width++;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  result,err:=(SQLExportDocumentCatalog{}).LookupExportStatsPersonOrigin(ctx,entry,ref,raw,owned)
  if err==nil||result!=(ExportStatsPersonOrigin{}){t.Fatal("invalid evidence reached locator",err)}
 })}
}
