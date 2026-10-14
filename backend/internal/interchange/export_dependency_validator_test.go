package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type exportSemanticsStub struct{calls int;deny bool}
func(s *exportSemanticsStub)QualifyExportDependency(_ context.Context,_ v2input.Entry,raw []byte,hints []v2draft.LayerRef)error{
 s.calls++;raw[0]='!';if len(hints)>0{hints[0].Ref.SHA256="changed"};if s.deny{return v2input.ErrInvalid};return nil
}
func TestExportDependencyValidatorRequiresSemanticsAndOwnsHints(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("master"))}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref,CanvasHash:rawSHA([]byte("canvas"))}
 raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"source":binding,"layers":[]any{}});if err!=nil{t.Fatal(err)}
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"LayerStack"}
 for _,mode:=range []string{"valid","deny","missing","hash"}{t.Run(mode,func(t *testing.T){
  semantics:=&exportSemanticsStub{deny:mode=="deny"};validator:=ExportDependencyDocumentValidator{Semantics:semantics};body:=append([]byte(nil),raw...)
  if mode=="missing"{validator.Semantics=nil};if mode=="hash"{body=append(body,' ')}
  children,err:=validator.ExportDocumentChildren(context.Background(),entry,body)
  if mode=="valid"{if err!=nil||len(children)!=1||children[0].Ref!=ref||rawSHA(body)!=entry.Ref.SHA256{t.Fatal("dependency context alias",err)}}else if err==nil{t.Fatal("unqualified dependency accepted")}
  if mode=="missing"||mode=="hash"{if semantics.calls!=0{t.Fatal("invalid bytes reached semantics")}}
 })}
}

func TestStatsExportRequiresBoundSourceAndSemanticAuthority(t *testing.T){
 for _,mode:=range []string{"valid","unbound","deny"}{t.Run(mode,func(t *testing.T){
  source:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))}
  value:=map[string]any{"source_image":source,"feature_version":"stats20_v1","values":make([]float64,20),"coverage_bins":make([]bool,512),"mask_confidence":1,"warnings":[]string{}}
  if mode=="unbound"{value["source_image"]=nil}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Stats20"}
  semantics:=&exportSemanticsStub{deny:mode=="deny"}
  children,err:=(ExportDependencyDocumentValidator{Semantics:semantics}).ExportDocumentChildren(context.Background(),entry,raw)
  if mode=="valid"{if err!=nil||len(children)!=1||children[0]!=(v2input.Entry{Ref:source,Contract:"WorkingImage"}){t.Fatal(children,err)}}else if err==nil{t.Fatal("unqualified stats accepted")}
  if mode=="unbound"&&semantics.calls!=0{t.Fatal("unbound stats reached authority")}
 })}
}
