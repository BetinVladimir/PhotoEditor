package v2draft
import("encoding/json";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/v2input")
func TestEditorBridgeReplacesOnlyOwnedOverride(t *testing.T){
 semantic:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)};old:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)};next:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)}
 raw,_:=json.Marshal(map[string]any{"overrides":[]v2input.Ref{semantic,old}});body,err:=replaceEditorBridgeRef(raw,&old,next);if err!=nil{t.Fatal(err)}
 var recipe struct{Overrides []v2input.Ref `json:"overrides"`};json.Unmarshal(body,&recipe);if len(recipe.Overrides)!=2||recipe.Overrides[0]!=semantic||recipe.Overrides[1]!=next{t.Fatal("unrelated override lost or old bridge retained")}
}
func TestEditorBridgeCompilesExactVerifiedPixelSource(t *testing.T){
 source:=strings.Repeat("a",64);body,err:=compileEditorBridgeOverride(json.RawMessage(`{"schema_version":1,"modules":{"strength":0}}`),source,7);if err!=nil{t.Fatal(err)}
 var envelope struct{Source string `json:"input_sha256"`;Revision int `json:"revision"`};json.Unmarshal(body,&envelope);if envelope.Source!=source||envelope.Revision!=7{t.Fatal("source/revision lost")}
 for _,bad:=range []string{`{"schema_version":1,"modules":{"strength":1},"extra":true}`,`{"schema_version":2,"modules":{"strength":1}}`,`{"schema_version":1,"modules":null}`} {if _,err:=compileEditorBridgeOverride(json.RawMessage(bad),source,7);err==nil{t.Fatal("invalid params admitted")}}
}
