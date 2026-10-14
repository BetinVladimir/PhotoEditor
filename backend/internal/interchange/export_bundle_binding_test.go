package interchange
import (
 "context"
 "encoding/json"
 "testing"
 "photoeditor/backend/internal/v2input"
)
type exportBundleBindingFactory struct{resolver ExportClosureNodeResolver;frozenRoots int;calls int}
func(s *exportBundleBindingFactory)BindExportGraph(_ context.Context,compiled CompiledExportPlan)(ExportClosureNodeResolver,error){
 s.calls++;for _,root:=range compiled.roots{if root.Contract=="FrozenRenderRecipe"{s.frozenRoots++}}
 return s.resolver,nil
}
func(s *exportBundleBindingFactory)ResolveExportNode(context.Context,v2input.Entry)(ExportClosureNode,error){return ExportClosureNode{},v2input.ErrInvalid}

func TestBundleGraphBindingRetainsFrozenParents(t *testing.T){
 // Build from the existing owned execution fixture; graph-only documents do
 // not prove schema/native qualification, but do expose binding order.
 tid,who,intent,oldFrozen,resolver:=exportExecutionFixture(t)
 var payload exportFreezePayload;if err:=json.Unmarshal(oldFrozen.Body(),&payload);err!=nil{t.Fatal(err)}
 var old importPlanDocument;if err:=decodeImportDocument([]byte(payload.PlanJCS),&old);err!=nil{t.Fatal(err)}
 request:=intent.Request;request.Format="photoeditor_bundle"
 intent,err:=FreezeCreateIntent(tid,who,request,"bundle-binding");if err!=nil{t.Fatal(err)}
 frame:=old.Entries[0]
 compiled,err:=CompileExportPlan(context.Background(),intent.Identity.Workflow,request.ProjectID,request.Format,payload.Snapshot,[]byte(payload.SnapshotJCS),ExportLimits{old.Archive,old.Expanded,old.Files},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return ExportFrame{ExternalID:frame.External,Path:frame.Path,Content:frame.Content,PortableRecipe:*frame.Recipe,Rating:frame.Rating,Pick:frame.Pick,Crop:frame.Crop},nil}));if err!=nil{t.Fatal(err)}
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,payload.Snapshot,[]byte(payload.SnapshotJCS));if err!=nil{t.Fatal(err)}
 factory:=&exportBundleBindingFactory{resolver:resolver}
 _,err=ResolveExportBundleSourceInventory(context.Background(),intent,frozen,factory)
 if err==nil{t.Fatal("synthetic {} frozen recipe passed actual schema qualification")}
 if factory.calls!=1||factory.frozenRoots<1{t.Fatal("portable root pruning lost frozen graph scope")}
}
