package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type exportNodeResolverStub struct{nodes map[uuid.UUID]ExportClosureNode;calls int}
func(s *exportNodeResolverStub)ResolveExportNode(_ context.Context,entry v2input.Entry)(ExportClosureNode,error){s.calls++;return s.nodes[entry.Ref.ArtifactID],nil}
func exportClosureFixture(t *testing.T)(CompiledExportPlan,*exportNodeResolverStub){
 t.Helper();document:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
 master,frozen,portable:=document(),document(),document()
 item:=v2input.SessionSnapshotItem{Ordinal:0,Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:master,CanvasHash:rawSHA([]byte("canvas"))},WorkingImage:master,Recipe:frozen}
 _,snapshot,err:=v2input.CompilePinnedSessionInputSnapshot(uuid.New(),[]v2input.SessionSnapshotItem{item});if err!=nil{t.Fatal(err)}
 sref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(snapshot)}
 content:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}
 compiled,err:=CompileExportPlan(context.Background(),uuid.New(),uuid.New(),"xmp",sref,snapshot,ExportLimits{1<<20,1<<20,10},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return ExportFrame{ExternalID:"one",Path:"one.tiff",Content:content,PortableRecipe:portable,Pick:"unmarked"},nil}));if err!=nil{t.Fatal(err)}
 entries:=map[uuid.UUID]v2input.Entry{};for _,root:=range compiled.roots{entries[root.Ref.ArtifactID]=root}
 resolver:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{}}
 for _,entry:=range compiled.roots{
  body:=[]byte(`{}`)
  switch entry.Contract{case "InterchangePlan":body=compiled.planBody;case "Params":body=compiled.paramsBody;case "InputSnapshot":body=snapshot;case "EncodedImage":body=nil}
  children:=[]v2input.Entry{}
  if body!=nil{refs,err:=importDocumentRefs(body,entry.Contract);if err!=nil{t.Fatal(err)};for id:=range refs{children=append(children,entries[id])}}
  resolver.nodes[entry.Ref.ArtifactID]=ExportClosureNode{Entry:entry,Body:body,Children:children}
 }
 return compiled,resolver
}
func TestExportClosureResolvesAllFrozenRootsAndOwnsInventory(t *testing.T){
 compiled,resolver:=exportClosureFixture(t)
 inventory,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);if err!=nil{t.Fatal(err)}
 if len(inventory.Entries())!=7||resolver.calls!=7{t.Fatal("incomplete inventory",resolver.calls)}
 entries:=inventory.Entries();entries[0].Contract="changed";if inventory.Entries()[0].Contract=="changed"{t.Fatal("inventory alias")}
}
func TestExportClosureRejectsOmittedOrMistypedPlanChildren(t *testing.T){
 for _,mode:=range []string{"omitted","mistyped","binary_json"}{t.Run(mode,func(t *testing.T){
  compiled,resolver:=exportClosureFixture(t);node:=resolver.nodes[compiled.planRef.ArtifactID]
  switch mode{case "omitted":node.Children=node.Children[:len(node.Children)-1];case "mistyped":node.Children[0].Contract="Artifact";case "binary_json":node.Body=nil;node.Children=nil}
  resolver.nodes[compiled.planRef.ArtifactID]=node
  if _,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);err==nil{t.Fatal("invalid closure accepted")}
 })}
}
func TestExportClosureCancellationBeforeResolution(t *testing.T){
 compiled,resolver:=exportClosureFixture(t);ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=ResolveExportInputInventory(ctx,compiled,resolver);err!=context.Canceled||resolver.calls!=0{t.Fatal("cancelled resolution",err)}
}
