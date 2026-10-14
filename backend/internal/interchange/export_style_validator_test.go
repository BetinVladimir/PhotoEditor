package interchange
import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type exportStyleProbeStub struct{runtime string;calls int;err error}
func(s *exportStyleProbeStub)RunExportNativeProbe(_ context.Context,raw []byte)([]byte,error){
 s.calls++;var request struct{Schema int `json:"schema_version"`;Contract string `json:"contract"`;Path string `json:"file_path"`;PolicyPath string `json:"model_policy_path"`;SHA string `json:"sha256"`;Bytes int64 `json:"bytes"`;Policy string `json:"model_policy_sha256"`}
 if err:=json.Unmarshal(raw,&request);err!=nil{return nil,err}
 for _,path:=range []string{request.Path,request.PolicyPath}{if _,err:=os.Stat(path);err!=nil{return nil,err}}
 if s.err!=nil{return nil,s.err}
 contract:=request.Contract;if contract==""{contract="StyleV2"}
 schemas:=map[string]int{"StyleV2":1,"Override":2,"RenderComposition":3,"ResolvedStyle":4,"ResolvedOrigin":5,"OperationParams":6,"SeriesCorrections":7,"SeriesOrigin":8,"SeriesMeasureOrigin":9,"MaskOrigin":10,"BodyOrigin":11,"BodyDependencyOrigin":12,"BodyFitOrigin":13,"PairAlignmentOrigin":14,"FaceDetectorOrigin":15}
 if schemas[contract]==0||request.Schema!=schemas[contract]||(contract=="StyleV2"&&request.Contract!=""){return nil,v2input.ErrInvalid}
 return canonical.Marshal(ExportNativeStyleReceipt{Schema:1,Contract:contract,SHA:request.SHA,Bytes:request.Bytes,Policy:request.Policy,Runtime:s.runtime})
}
// Mock probe proves staging and mandatory authority, not full StyleV2 schema.
func TestNativeStyleValidatorStagesPolicyAndPreservesAuthority(t *testing.T){
 for _,mode:=range []string{"valid","hash","budget","probe","cancelled"}{t.Run(mode,func(t *testing.T){
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,1000);if err!=nil{t.Fatal(err)}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime}
  authority:=&importValidatorStub{};models:=map[string]string{"model":"weight"}
  validator,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportReaderValidator{authority});if err!=nil{t.Fatal(err)}
  models["model"]="changed";if string(validator.policy)!=`{"model":"weight"}`{t.Fatal("model policy alias")}
  raw:=[]byte(`{}`);entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"StyleV2"};ctx:=context.Background();cause:=errors.New("schema refused")
  switch mode{case "hash":raw=[]byte(`{"other":1}`);case "budget":scratch.maximum=1;case "probe":probe.err=cause;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  _,err=validator.ExportDocumentChildren(ctx,entry,raw)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected validation: %v",err)}
  if mode=="valid"&&(probe.calls!=1||authority.calls!=1){t.Fatal("missing schema or authority")}
  if mode!="valid"&&authority.calls!=0{t.Fatal("invalid schema reached authority")}
  files,readErr:=os.ReadDir(root);if readErr!=nil||len(files)!=0||scratch.used!=0{t.Fatal("staging leaked",readErr,scratch.used)}
 })}
}

func TestNativeOverrideSchemaStagesAndRequiresAuthority(t *testing.T) {
 for _, mode := range []string{"valid", "probe", "hash", "cancelled"} { t.Run(mode, func(t *testing.T) {
  root := t.TempDir()
  scratch := &ExportArchiveScratch{root:root, maximum:1<<20}
  runtime := rawSHA([]byte("runtime"))
  probe := &exportStyleProbeStub{runtime:runtime}
  authority := &importValidatorStub{}
  validator, err := NewNativeExportOverrideValidator(scratch, probe, map[string]string{}, runtime, exportReaderValidator{authority}); if err != nil { t.Fatal(err) }
  raw := []byte(`{"input_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","modules":{"strength":0.7},"revision":1,"schema_version":1}`)
  entry := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(raw)}, Contract:"Override"}
  ctx := context.Background()
  switch mode {
  case "probe": probe.err = errors.New("invalid replacement modules")
  case "hash": raw = append(raw, ' ')
  case "cancelled": var cancel context.CancelFunc; ctx, cancel = context.WithCancel(ctx); cancel()
  }
  _, err = validator.ExportDocumentChildren(ctx, entry, raw)
  if (err == nil) != (mode == "valid") { t.Fatalf("unexpected override schema: %v", err) }
  expected := 0; if mode == "valid" { expected = 1 }
  if authority.calls != expected { t.Fatal("schema bypassed mandatory authority") }
  files, err := os.ReadDir(root); if err != nil || len(files) != 0 || scratch.used != 0 { t.Fatal("override staging leaked", err) }
 }) }
}

func TestNativeResolvedSchemaStagesAndRequiresAuthority(t *testing.T) {
 for _, mode := range []string{"valid", "probe", "hash", "cancelled"} { t.Run(mode, func(t *testing.T) {
  root := t.TempDir()
  scratch := &ExportArchiveScratch{root:root, maximum:1<<20}
  runtime := rawSHA([]byte("runtime"))
  probe := &exportStyleProbeStub{runtime:runtime}
  authority := &resolvedChildrenAuthorityStub{}
  validator, err := NewNativeExportResolvedValidator(scratch, probe, map[string]string{}, runtime, authority); if err != nil { t.Fatal(err) }
  fixture, err := os.ReadFile("../review/testdata/resolved-binding-python.json"); if err != nil { t.Fatal(err) }
  var document map[string]any
  if err:=json.Unmarshal(fixture,&document);err!=nil{t.Fatal(err)}
  document["params"].(map[string]any)["style"].(map[string]any)["artifact_id"]=uuid.New().String()
  delete(document,"hash");hash,err:=canonical.Hash(document);if err!=nil{t.Fatal(err)};document["hash"]=hash
  raw, err := canonical.Marshal(document); if err != nil { t.Fatal(err) }
  entry := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(raw)}, Contract:"ResolvedStyle"}
  ctx := context.Background()
  switch mode {
  case "probe": probe.err = errors.New("invalid replacement modules")
  case "hash": raw = append(raw, ' ')
  case "cancelled": var cancel context.CancelFunc; ctx, cancel = context.WithCancel(ctx); cancel()
  }
  _, err = validator.ExportDocumentChildren(ctx, entry, raw)
  if (err == nil) != (mode == "valid") { t.Fatalf("unexpected resolved schema: %v", err) }
  expected := 0; if mode == "valid" { expected = 1 }
  if authority.calls != expected { t.Fatal("schema bypassed mandatory authority") }
  files, err := os.ReadDir(root); if err != nil || len(files) != 0 || scratch.used != 0 { t.Fatal("resolved staging leaked", err) }
 }) }
}

type resolvedChildrenAuthorityStub struct{calls int}
func(s *resolvedChildrenAuthorityStub)ExportDocumentChildren(_ context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){s.calls++;return exportResolvedChildren(entry,raw)}

// Envelope coverage only: production content validation belongs to the Python probe.
func TestNativeExportContractEnvelopeAndUnknownRefusal(t *testing.T){
 for _,contract:=range []string{"StyleV2","Override","RenderComposition","ResolvedOrigin","OperationParams","SeriesCorrections","SeriesOrigin","SeriesMeasureOrigin","MaskOrigin","BodyOrigin","BodyDependencyOrigin","BodyFitOrigin","PairAlignmentOrigin","FaceDetectorOrigin","Unknown"}{t.Run(contract,func(t *testing.T){
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};authority:=&importValidatorStub{}
  validator,err:=NewNativeExportStyleValidator(scratch,probe,map[string]string{},runtime,exportReaderValidator{authority});if err!=nil{t.Fatal(err)}
  validator.contract=contract;raw:=[]byte(`{}`)
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:contract}
  _,err=validator.ExportDocumentChildren(context.Background(),entry,raw)
  if contract=="Unknown"{if err==nil||probe.calls!=0||authority.calls!=0{t.Fatal("unknown contract reached native IO or authority")}}else if err!=nil||probe.calls!=1||authority.calls!=1{t.Fatal("contract envelope refused or skipped",err)}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("staging leaked",err)}
 })}
}
