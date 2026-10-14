package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestExecutableImportDocumentsOwnEvidenceAndPublishInOrder(t *testing.T){
 workflow,project,parsedRef,parsedRaw,sources:=importSourcesFixture(t)
 var parsed importParsedDocument;if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{t.Fatal(err)};original:=parsed.Manifest.Entries[0]
 plan:=importPlanDocument{Schema:1,Project:project,Direction:"import",Format:"photoeditor_bundle",Snapshot:rawSHA([]byte("snapshot")),Archive:1024,Expanded:2048,Files:10,
  Entries:[]importPlanEntry{{ID:original.ID,External:original.External,Path:original.ImagePath,Content:parsed.Files[0].Content,Rating:original.Rating,Pick:original.Pick,Crop:original.Crop}}}
 planRaw,err:=canonical.Marshal(plan);if err!=nil{t.Fatal(err)}
 identity:=ImportDocumentIdentity{Run:uuid.New(),Workflow:workflow};base,err:=FreezeImportDocuments(identity,parsedRef,planRaw);if err!=nil{t.Fatal(err)}
 evidence:=importEvidenceDocument{Schema:1,Workflow:workflow,Project:project,Plan:base.plan.Ref,Parsed:parsedRef,Sources:sources,
  Metadata:importMetadataDocument{Schema:1,Workflow:workflow,Project:project,Parsed:parsedRef,Collection:uuid.NewSHA1(workflow,[]byte("interchange:draft-collection:1")),Entries:[]importMetadataReceipt{}},Diagnostics:[]importRecipeDiagnostic{}}
 raw,err:=canonical.Marshal(evidence);if err!=nil{t.Fatal(err)}
 documents,err:=FreezeImportExecutionDocuments(context.Background(),identity,parsedRef,planRaw,parsedRaw,raw);if err!=nil{t.Fatal(err)}
 raw[0]='x';var params importParamsDocument;if err:=decodeImportDocument(documents.params.Body,&params);err!=nil{t.Fatal(err)}
 if params.Evidence==nil||*params.Evidence!=documents.evidence.Ref{t.Fatal("missing evidence pin")}
 publisher:=&importDocumentPublisherStub{}
 if _,err:=documents.Publish(context.Background(),publisher);err!=nil{t.Fatal(err)}
 if len(publisher.calls)!=3||publisher.calls[0].Purpose!="v30_interchange_import_plan"||publisher.calls[1].Purpose!="v30_interchange_import_evidence"||publisher.calls[2].Purpose!="task_params"{t.Fatal("wrong dependency order")}
 entries:=[]v2input.Entry{{Ref:documents.params.Ref,Contract:"Params"},{Ref:documents.plan.Ref,Contract:"InterchangePlan"},{Ref:parsedRef,Contract:"ParsedInterchange"},{Ref:documents.evidence.Ref,Contract:"FrozenInterchangeImportEvidence"}}
 if _,err:=FreezeImportInputs(uuid.New(),documents,entries);err!=nil{t.Fatal(err)}
 if _,err:=FreezeImportInputs(uuid.New(),documents,entries[:3]);err==nil{t.Fatal("evidence omitted from closure")}
}
