package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
func TestExportFrozenStyleScopeRequiresActualSelectedBaseStyle(t *testing.T){
 for _,mode:=range []string{"valid","foreign_style","binding","hash","child","revoked","cancelled"}{t.Run(mode,func(t *testing.T){
  ref:=func(label string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(label))}}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref("source"),CanvasHash:rawSHA([]byte("canvas"))}
  styleRaw:=[]byte(`{}`);style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(styleRaw)}
  authored,err:=canonical.Marshal(map[string]any{"schema_version":3,"source":binding,"base_style":style,"session_corrections":nil,"learning_version_id":nil,"overrides":[]v2input.Ref{},"layer_stack":nil,"renderer_version":"v2.2","parameter_delta":v2draft.CorrectionVector{}});if err!=nil{t.Fatal(err)}
  raw,err:=v2draft.BuildFrozenRenderRecipe(authored,v2draft.ResolvedRecipeInputs{Binding:binding,BaseStyle:style,Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1},Saturation:1},ICCLockSHA:rawSHA([]byte("icc")),ModelLockSHA:rawSHA([]byte("model"))},nil);if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"FrozenRenderRecipe"}
  entry:=v2input.Entry{Ref:style,Contract:"StyleV2"}
  reader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:raw,Children:[]v2input.Entry{entry}}}
  policy:=&importValidatorStub{}
  scope:=&ExportFrozenStyleScope{reader:reader,recipes:[]exportRecipeBinding{{ref:parent.Ref,binding:binding}},policy:exportReaderValidator{policy}}
  cause:=errors.New("recipe authority revoked");ctx:=context.Background()
  switch mode{case "foreign_style":entry.Ref.ArtifactID=uuid.New();case "binding":scope.recipes[0].binding.AssetVersionID=uuid.New();case "hash":reader.node.Body=[]byte("{}");case "child":reader.node.Children=nil;case "revoked":reader.err=cause;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  _,err=scope.ExportDocumentChildren(ctx,entry,styleRaw)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected style scope: %v",err)}
  if mode=="valid"&&policy.calls!=1{t.Fatal("mandatory policy skipped")}
  if mode!="valid"&&policy.calls!=0{t.Fatal("unscoped style reached policy")}
  if mode=="revoked"&&!errors.Is(err,cause){t.Fatal("revocation cause lost")}
 })}
}
