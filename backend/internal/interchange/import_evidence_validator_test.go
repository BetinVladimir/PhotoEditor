package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestImportEvidenceValidatorProvesTerminalScopeAndExactClosure(t *testing.T){
 for _,change:=range []string{"none","project","plan","parsed","source","format","metadata","diagnostic","identity"}{t.Run(change,func(t *testing.T){
  workflow,project,parsedRef,parsedRaw,sources:=importSourcesFixture(t)
  var parsed importParsedDocument;if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{t.Fatal(err)};original:=parsed.Manifest.Entries[0]
  plan:=importPlanDocument{Schema:1,Project:project,Direction:"import",Format:"photoeditor_bundle",Snapshot:rawSHA([]byte("snapshot")),Archive:1024,Expanded:2048,Files:10,
   Entries:[]importPlanEntry{{ID:original.ID,External:original.External,Path:original.ImagePath,Content:parsed.Files[0].Content,Rating:original.Rating,Pick:original.Pick,Crop:original.Crop}}}
  planRaw,err:=canonical.Marshal(plan);if err!=nil{t.Fatal(err)}
  validator,err:=NewImportScopedDocumentValidator(project,plan.Snapshot,planRaw,parsedRaw);if err!=nil{t.Fatal(err)}
  task:=uuid.NewSHA1(workflow,[]byte("v2.interchange.import:0"))
  document:=importEvidenceDocument{Schema:1,Workflow:workflow,Project:project,Plan:v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("interchange:plan:1")),SHA256:rawSHA(planRaw)},Parsed:parsedRef,Sources:sources,
   Metadata:importMetadataDocument{Schema:1,Workflow:workflow,Project:project,Parsed:parsedRef,Collection:uuid.NewSHA1(workflow,[]byte("interchange:draft-collection:1")),Entries:[]importMetadataReceipt{}},Diagnostics:[]importRecipeDiagnostic{}}
  switch change{case "project":document.Project=uuid.New();case "plan":document.Plan.SHA256=rawSHA([]byte("foreign"));case "parsed":document.Parsed.ArtifactID=uuid.New();case "source":document.Sources.Entries[0].Reason=nil;case "format":document.Sources.Format="xmp";case "metadata":document.Metadata.Collection=uuid.New();case "diagnostic":document.Diagnostics=append(document.Diagnostics,importRecipeDiagnostic{ID:original.ID,Reason:"unrelated"})}
  raw,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("interchange:evidence:1")),SHA256:rawSHA(raw)},Contract:"FrozenInterchangeImportEvidence"}
  if change=="identity"{entry.Ref.ArtifactID=uuid.New()}
  children,err:=validator.ImportDocumentChildren(context.Background(),entry,raw)
  if change!="none"{if err==nil{t.Fatal("inconsistent evidence accepted")};return}
  if err!=nil||len(children)!=2{t.Fatal("evidence closure",err)}
  found:=map[string]v2input.Ref{};for _,child:=range children{found[child.Contract]=child.Ref}
  if found["InterchangePlan"]!=document.Plan||found["ParsedInterchange"]!=parsedRef{t.Fatal("wrong evidence roots")}
 })}
}
