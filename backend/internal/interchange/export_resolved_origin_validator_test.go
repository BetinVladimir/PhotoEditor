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
type resolvedOriginSequence struct{evidence ExportResolvedOriginEvidence;calls int;change bool}
func(s *resolvedOriginSequence)LookupExportResolvedOrigin(context.Context,v2input.Entry,[]byte)(ExportResolvedOriginEvidence,error){s.calls++;out:=s.evidence;if s.change&&s.calls>1{out.Object.VersionID="changed"};return out,nil}
type resolvedOriginTypedStub struct{}
func(resolvedOriginTypedStub)ExportDocumentChildren(_ context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){return exportResolvedChildren(entry,raw)}
// Mock receipts test orchestration, not production Python schema semantics.
func TestResolvedOriginValidatorRechecksOriginalParamsAndCleans(t *testing.T){
 for _,mode:=range []string{"valid","changed","bytes","probe"}{t.Run(mode,func(t *testing.T){
  params,err:=canonical.Marshal(map[string]any{"image":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}});if err!=nil{t.Fatal(err)}
  raw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"image":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}}});if err!=nil{t.Fatal(err)}
  origin:=&resolvedOriginSequence{evidence:ExportResolvedOriginEvidence{Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)},Object:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:rawSHA(params),Bytes:int64(len(params)),ContentType:"application/json"}},change:mode=="changed"}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,1<<20);if err!=nil{t.Fatal(err)}
  streamed:=append([]byte(nil),params...);if mode=="bytes"{streamed[0]='!'}
  validator,err:=NewExportResolvedOriginValidator(resolvedOriginTypedStub{},origin,statsParamsObjects{streamed},scratch,probe,map[string]string{},runtime);if err!=nil{t.Fatal(err)}
  children,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ResolvedStyle"},raw)
  if mode=="valid"{if err!=nil||len(children)!=1||origin.calls!=2||probe.calls!=1{t.Fatal("origin path incomplete",err)}}else if err==nil||children!=nil{t.Fatal("invalid origin admitted",err)}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
 })}
}

type resolvedOriginOtherSpy struct{calls int}
func(s *resolvedOriginOtherSpy)ExportDocumentChildren(_ context.Context,entry v2input.Entry,_ []byte)([]v2input.Entry,error){s.calls++;return []v2input.Entry{entry},nil}
func TestResolvedOriginValidatorDelegatesOrdinaryDocumentsWithoutOriginIO(t *testing.T){
 scratch,err:=NewExportArchiveScratch(t.TempDir(),1000);if err!=nil{t.Fatal(err)}
 other:=&resolvedOriginOtherSpy{};origin:=&resolvedOriginSequence{}
 validator,err:=NewExportResolvedOriginValidator(other,origin,exportDefectNoIO{t},scratch,&exportStyleProbeStub{},map[string]string{},rawSHA([]byte("runtime")));if err!=nil{t.Fatal(err)}
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("{}"))},Contract:"StyleV2"}
 result,err:=validator.ExportDocumentChildren(context.Background(),entry,[]byte("{}"))
 if err!=nil||len(result)!=1||result[0]!=entry||other.calls!=1||origin.calls!=0{t.Fatal("ordinary delegation changed",err)}
}
