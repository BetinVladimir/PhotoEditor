package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestPreparedRegistryDelegatesEveryReadAndPreservesCurrentRefusal(t *testing.T){
 registry:=&ImportPreparedNodeRegistry{}
 entry:=v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}}
 calls:=0;sentinel:=errors.New("scope revoked")
 resolver:=preparedRecipeNodeFunc(func(_ context.Context,e v2input.Entry)(ImportClosureNode,error){calls++;if calls>1{return ImportClosureNode{},sentinel};return ImportClosureNode{Entry:e},nil})
 if err:=registry.register(map[v2input.Entry]ImportClosureNodeResolver{entry:resolver});err!=nil{t.Fatal(err)}
 router:=ImportNodeResolverRouter{Prepared:registry}
 if node,err:=router.ResolveImportNode(context.Background(),entry);err!=nil||node.Entry!=entry{t.Fatal("dispatch",err)}
 if _,err:=router.ResolveImportNode(context.Background(),entry);!errors.Is(err,sentinel)||calls!=2{t.Fatal("cached authority",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=router.ResolveImportNode(ctx,entry);!errors.Is(err,context.Canceled)||calls!=2{t.Fatal("cancel invoked resolver",err)}
 foreign:=entry;foreign.Ref.SHA256=rawSHA([]byte("foreign"))
 if registry.Contains(foreign){t.Fatal("hash wildcard")}
 if _,err:=registry.ResolveImportNode(context.Background(),foreign);err==nil{t.Fatal("unknown identity accepted")}
}
func TestPreparedRegistryRefusesAliasWithoutPartialRegistration(t *testing.T){
 registry:=&ImportPreparedNodeRegistry{}
 first:=v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("first"))}}
 resolver:=preparedRecipeNodeFunc(func(_ context.Context,e v2input.Entry)(ImportClosureNode,error){return ImportClosureNode{Entry:e},nil})
 if err:=registry.register(map[v2input.Entry]ImportClosureNodeResolver{first:resolver});err!=nil{t.Fatal(err)}
 alias:=first;alias.Contract="DenseWarpField"
 extra:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
 if err:=registry.register(map[v2input.Entry]ImportClosureNodeResolver{alias:resolver,extra:resolver});err==nil{t.Fatal("cross-role alias accepted")}
 if registry.Contains(alias)||registry.Contains(extra)||!registry.Contains(first){t.Fatal("partial mutation on refusal")}
}
func TestPreparedRegistryReplayRetainsOriginalAuthorityAndRejectsForeignResult(t *testing.T){
 entry:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
 registry:=&ImportPreparedNodeRegistry{}
 denied:=errors.New("original scope revoked")
 original:=preparedRecipeNodeFunc(func(context.Context,v2input.Entry)(ImportClosureNode,error){return ImportClosureNode{},denied})
 permissive:=preparedRecipeNodeFunc(func(_ context.Context,e v2input.Entry)(ImportClosureNode,error){return ImportClosureNode{Entry:e},nil})
 if err:=registry.register(map[v2input.Entry]ImportClosureNodeResolver{entry:original});err!=nil{t.Fatal(err)}
 if err:=registry.register(map[v2input.Entry]ImportClosureNodeResolver{entry:permissive});err!=nil{t.Fatal(err)}
 if _,err:=registry.ResolveImportNode(context.Background(),entry);!errors.Is(err,denied){t.Fatal("replay replaced authority",err)}
 other:=entry;other.Ref.ArtifactID=uuid.New()
 foreign:=preparedRecipeNodeFunc(func(context.Context,v2input.Entry)(ImportClosureNode,error){return ImportClosureNode{Entry:entry},nil})
 if err:=registry.register(map[v2input.Entry]ImportClosureNodeResolver{other:foreign});err!=nil{t.Fatal(err)}
 if _,err:=registry.ResolveImportNode(context.Background(),other);err==nil{t.Fatal("foreign resolver result accepted")}
 opaque:=entry;opaque.Ref.ArtifactID=uuid.New();opaque.Contract="Artifact"
 if err:=registry.register(map[v2input.Entry]ImportClosureNodeResolver{opaque:permissive});err==nil{t.Fatal("opaque route can override canonical admission")}
}
// Mock leaves exercise registration ordering; they do not prove StyleV2 or
// WorkingImage semantics. Those remain the production resolver's obligation.
func TestPreparedRecipeRegistryRequiresCompleteQualificationAndFreshRootRead(t *testing.T){
 for _,mode:=range []string{"valid","dependency_refusal","changed_root"}{t.Run(mode,func(t *testing.T){
  scope:=ImportPreparedScope{Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Entry:uuid.New()}
  leaf:=[]byte(`{}`)
  scope.Binding=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(leaf)},CanvasHash:rawSHA([]byte("canvas"))}
  style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(leaf)}
  raw,err:=canonical.Marshal(map[string]any{"schema_version":3,"source":scope.Binding,"base_style":style,"session_corrections":nil,"learning_version_id":nil,"overrides":[]v2input.Ref{},"layer_stack":nil,"protected_mask":nil,"renderer_version":"v2.2","parameter_delta":map[string]any{"ev":0,"log_r":0,"log_g":0,"log_b":0,"saturation_delta":0,"retouch_delta":0}});if err!=nil{t.Fatal(err)}
  ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(scope.Workflow,[]byte("interchange:recipe:"+scope.Entry.String())),SHA256:rawSHA(raw)}
  registry:=&ImportPreparedNodeRegistry{}
  catalog:=&importCatalogStub{object:objectstore.Object{Key:"root",VersionID:"v1",SHA256:ref.SHA256,Bytes:int64(len(raw)),ContentType:"application/json"}}
  objects:=&importReaderStub{body:raw}
  failure:=errors.New("dependency denied")
  calls:=0
  resolver:=preparedRecipeNodeFunc(func(_ context.Context,e v2input.Entry)(ImportClosureNode,error){calls++;if mode=="dependency_refusal"{return ImportClosureNode{},failure};return ImportClosureNode{Entry:e,Body:append([]byte(nil),leaf...)},nil})
  q:=registeredPreparedImportRecipeQualifier{Scope:scope,Resolver:resolver,Catalog:catalog,Objects:objects,Registry:registry}
  err=q.QualifyPreparedImportRecipe(context.Background(),ref,raw,scope.Binding)
  root:=v2input.Entry{Ref:ref,Contract:"EditingRecipeV3"}
  if mode=="dependency_refusal"{if !errors.Is(err,failure)||registry.Contains(root)||registry.Contains(v2input.Entry{Ref:style,Contract:"StyleV2"}){t.Fatal("failed qualification registered nodes")};return}
  if err!=nil||calls!=4||catalog.calls!=0||objects.opens!=0{t.Fatal("registration preceded complete qualification or read unpublished root",err)}
  if mode=="changed_root"{catalog.change=true}
  node,err:=registry.ResolveImportNode(context.Background(),root)
  if mode=="valid"{if err!=nil||node.Entry!=root||rawSHA(node.Body)!=ref.SHA256||catalog.calls!=2||objects.opens!=1{t.Fatal("published root not physically re-read",err)}}else if err==nil{t.Fatal("changed root accepted")}
 })}
}
