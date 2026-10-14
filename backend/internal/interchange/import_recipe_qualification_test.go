package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type preparedRecipeNodeFunc func(context.Context,v2input.Entry)(ImportClosureNode,error)
func(f preparedRecipeNodeFunc)ResolveImportNode(c context.Context,e v2input.Entry)(ImportClosureNode,error){return f(c,e)}
func TestPreparedRecipeQualificationRepeatsExactPhysicalDependencies(t *testing.T){
 for _,mode:=range []string{"valid","changed_dependency","json_as_binary","foreign_entry","resolver_refusal","foreign_binding","cancelled_second_pass"}{t.Run(mode,func(t *testing.T){
  scope:=ImportPreparedScope{Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Entry:uuid.New()}
  leaf:=[]byte(`{}`);sha:=rawSHA(leaf)
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:rawSHA([]byte("canvas"))};scope.Binding=binding
  style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
  raw,err:=canonical.Marshal(map[string]any{"schema_version":3,"source":binding,"base_style":style,"session_corrections":nil,"learning_version_id":nil,"overrides":[]v2input.Ref{},"layer_stack":nil,"protected_mask":nil,"renderer_version":"v2.2","parameter_delta":map[string]any{"ev":0,"log_r":0,"log_g":0,"log_b":0,"saturation_delta":0,"retouch_delta":0}});if err!=nil{t.Fatal(err)}
  ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(scope.Workflow,[]byte("interchange:recipe:"+scope.Entry.String())),SHA256:rawSHA(raw)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();calls:=0;sentinel:=errors.New("physical authority refused")
  resolver:=preparedRecipeNodeFunc(func(_ context.Context,e v2input.Entry)(ImportClosureNode,error){
   calls++;node:=ImportClosureNode{Entry:e,Body:append([]byte(nil),leaf...)}
   switch mode{case "changed_dependency":if calls>2{node.Body=[]byte(`{"changed":true}`)};case "json_as_binary":node.Body=nil;case "foreign_entry":node.Entry.Ref.ArtifactID=uuid.New();case "resolver_refusal":return ImportClosureNode{},sentinel;case "cancelled_second_pass":if calls>2{cancel()}}
   return node,nil
  })
  actual:=binding;if mode=="foreign_binding"{actual.AssetVersionID=uuid.New()}
  err=(BoundImportPreparedRecipeQualifier{Scope:scope,Resolver:resolver}).QualifyPreparedImportRecipe(ctx,ref,raw,actual)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected recipe qualification",err)}
  if mode=="valid"&&calls!=4{t.Fatal("complete dependency graph not reread",calls)}
  if mode=="foreign_binding"&&calls!=0{t.Fatal("foreign binding reached physical IO")}
  if mode=="resolver_refusal"&&!errors.Is(err,sentinel){t.Fatal("physical refusal lost",err)}
  if mode=="cancelled_second_pass"&&err!=context.Canceled{t.Fatal("cancellation lost",err)}
 })}
}
