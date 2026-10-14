package interchange
import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func importTypedPlanFixture(t *testing.T)(ImportPlanDocumentValidator,importPlanDocument){
 project:=uuid.New();snapshot:=rawSHA([]byte("snapshot"))
 plan:=importPlanDocument{Schema:1,Project:project,Direction:"import",Format:"photoeditor_bundle",Snapshot:snapshot,
 Entries:[]importPlanEntry{{ID:uuid.New(),External:"frame-1",Path:"originals/a.CR3",Content:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("original"))},Rating:0,Pick:"unmarked"}},Archive:1024,Expanded:2048,Files:10}
 return ImportPlanDocumentValidator{Project:project,SnapshotDigest:snapshot},plan
}
func validateTypedPlan(t *testing.T,v ImportPlanDocumentValidator,plan importPlanDocument)([]v2input.Entry,error){
 raw,err:=canonical.Marshal(plan);if err!=nil{t.Fatal(err)}
 return v.ImportDocumentChildren(context.Background(),v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"InterchangePlan"},raw)
}
func TestImportPlanValidatorPreservesTerminalEntryAndFindsOriginalInput(t *testing.T){
 v,plan:=importTypedPlanFixture(t);children,err:=validateTypedPlan(t,v,plan)
 if err!=nil||len(children)!=1||children[0].Ref!=plan.Entries[0].Content{t.Fatal("terminal plan",err)}
}
func TestImportPlanValidatorRejectsForeignScopeCollisionAndUnsafePath(t *testing.T){
 for _,change:=range []string{"scope","collision","path","recipe"}{
  v,plan:=importTypedPlanFixture(t)
  switch change{case "scope":plan.Project=uuid.New();case "collision":plan.Entries=append(plan.Entries,plan.Entries[0]);case "path":plan.Entries[0].Path="../escape";case "recipe":ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("recipe"))};plan.Entries[0].Recipe=&ref}
  if _,err:=validateTypedPlan(t,v,plan);err==nil{t.Fatal("invalid plan accepted",change)}
 }
}
func TestImportPlanValidatorRejectsMissingRequiredNullableField(t *testing.T){
 v,plan:=importTypedPlanFixture(t);raw,err:=canonical.Marshal(plan);if err!=nil{t.Fatal(err)}
 var wire map[string]any;if err:=json.Unmarshal(raw,&wire);err!=nil{t.Fatal(err)}
 entries:=wire["entries"].([]any);delete(entries[0].(map[string]any),"crop")
 raw,err=canonical.Marshal(wire);if err!=nil{t.Fatal(err)}
 if _,err:=v.ImportDocumentChildren(context.Background(),v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"InterchangePlan"},raw);err==nil{t.Fatal("missing required nullable field accepted")}
}
func TestImportParamsValidatorFindsExactRootReferences(t *testing.T){
 docs,_:=importMaterializationFixture(t);children,err:=(ImportPlanDocumentValidator{}).ImportDocumentChildren(context.Background(),v2input.Entry{Ref:docs.params.Ref,Contract:"Params"},docs.params.Body)
 if err!=nil||len(children)!=2||children[1].Ref!=docs.plan.Ref{t.Fatal("Params validation",err)}
}
