package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestImportRecipeValidatorInstallationRequiresFrozenPlan(t *testing.T){
 docs,_,_:=measuredImportFixture(t);var plan importPlanDocument
 if err:=decodeImportDocument(docs.plan.Body,&plan);err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","missing_dependencies","foreign_project","changed_plan","cancelled"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  project:=plan.Project;raw:=append([]byte(nil),docs.plan.Body...);var dependencies ImportTypedDocumentValidator=ImportPlanDocumentValidator{Project:project,SnapshotDigest:plan.Snapshot}
  switch mode{case "missing_dependencies":dependencies=nil;case "foreign_project":project=uuid.New();case "changed_plan":raw=[]byte(`{}`);case "cancelled":cancel()}
  validator,err:=NewImportRecipeDocumentValidator(ctx,docs.identity.Workflow,project,plan.Snapshot,raw,dependencies)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected recipe validator installation",err)}
  if mode=="valid"{
   _,err=validator.ImportDocumentChildren(ctx,v2input.Entry{Contract:"EditingRecipeV3",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}},[]byte(`{}`))
   if err==nil{t.Fatal("unplanned recipe accepted")}
  }
 })}
}
