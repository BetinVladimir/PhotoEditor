package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
func exportFreezeFixture(t *testing.T)(CreateIntent,CompiledExportPlan,v2input.Ref,[]byte){
 t.Helper();_,project,snapshotRef,raw,frame:=exportPlanFixture(t)
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument(raw,&snapshot);err!=nil{t.Fatal(err)}
 id:=snapshot.SnapshotID
 intent,err:=FreezeCreateIntent(uuid.New(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},CreateRequest{ProjectID:project,Direction:"export",Format:"xmp",SnapshotID:&id},"export-freeze-key");if err!=nil{t.Fatal(err)}
 compiled,err:=CompileExportPlan(context.Background(),intent.Identity.Workflow,project,"xmp",snapshotRef,raw,ExportLimits{1<<20,1<<20,10},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return frame,nil}));if err!=nil{t.Fatal(err)}
 return intent,compiled,snapshotRef,raw
}
func TestExportFreezeOwnsExactRequestAndDocumentBytes(t *testing.T){
 intent,compiled,snapshotRef,raw:=exportFreezeFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshotRef,raw);if err!=nil{t.Fatal(err)}
 var payload exportFreezePayload;if err:=decodeImportDocument(frozen.Body(),&payload);err!=nil{t.Fatal(err)}
 if payload.Identity!=intent.Identity||payload.Snapshot!=snapshotRef||payload.SnapshotJCS!=string(raw)||payload.RequestSHA!=intent.RequestSHA{t.Fatal("lost frozen identity")}
 body:=frozen.Body();body[0]='x';raw[0]='x'
 if frozen.Body()[0]!='{'||payload.SnapshotJCS[0]!='{'{t.Fatal("recovery payload alias")}
}
func TestExportFreezeRejectsCrossScopeDocuments(t *testing.T){
 for _,mode:=range []string{"request","task","snapshot","root","format"}{t.Run(mode,func(t *testing.T){
  intent,compiled,snapshotRef,raw:=exportFreezeFixture(t)
  switch mode{case "request":intent.RequestSHA=rawSHA([]byte("other"));case "task":intent.Identity.Task=uuid.New();case "snapshot":snapshotRef.SHA256=rawSHA([]byte("other"));case "root":compiled.roots=compiled.roots[1:];case "format":intent.Request.Format="photoeditor_bundle"}
  if _,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshotRef,raw);err==nil{t.Fatal("cross-scope freeze accepted")}
 })}
}

func TestExportFreezeRecoveryRecompilesExactDocuments(t *testing.T){
 intent,compiled,snapshot,raw:=exportFreezeFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 recovered,plan,err:=DecodeFrozenExportDocuments(context.Background(),intent,frozen.Body());if err!=nil{t.Fatal(err)}
 if string(recovered.Body())!=string(frozen.Body())||plan.TaskID()!=compiled.TaskID(){t.Fatal("recovery changed plan")}
}
