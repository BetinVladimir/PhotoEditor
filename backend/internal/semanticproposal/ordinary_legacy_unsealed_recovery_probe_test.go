package semanticproposal

import (
 "context"
 "encoding/json"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Construct the historical ordinary person checkpoint through its real retained
// constructor, interrupt transport, then recover without adding scene policy or
// rewriting accepted configuration. This is not a new public legacy admission.
func probeLegacyOrdinaryUnsealedRecovery(t *testing.T,ctx context.Context,p TransferDraftPreparer,who auth.Identity,tenant uuid.UUID,intent v2input.SemanticEditIntent) {
 t.Helper()
 if p.Configuration.Scene!=nil {t.Fatal("legacy fixture must omit scene policy")}
 key:="legacy-ordinary-unsealed-recovery"
 raw,err:=canonical.Marshal(intent);if err!=nil {t.Fatal(err)}
 request,err:=CompileRequest(tenant,who.UserID,key,raw);if err!=nil {t.Fatal(err)}
 published,err:=PublishIntent(ctx,p.Documents,who,request);if err!=nil {t.Fatal(err)}
 personRun,_,err:=personPreparationIDs(request.RunID);if err!=nil {t.Fatal(err)}
 failed:=p
 interruption:=&nativePersonFailTransport{Run:personRun,Delegate:p.TransportDocuments}
 failed.TransportDocuments=interruption
 if _,err=failed.prepareMissingCreatePerson(ctx,who,request,key,published);err==nil||interruption.Calls!=1 {t.Fatal("historical unsealed checkpoint interruption",err)}
 // The actual saved plan must still be unsealed; absence of a public parent
 // or response cannot be replaced by a child receipt.
 anchor,err:=p.Reader.loadPersonPreparationAnchor(ctx,who,tenant,request.RunID,p.Configuration.Color,p.Configuration.MetadataBytes);if err!=nil {t.Fatal(err)}
 state,err:=p.Reader.readNativeInputSavedStageState(ctx,who,request,published.Ref,anchor.Snapshot,"mask.person");if err!=nil||state.sealed||state.runState!="draft" {t.Fatal("legacy pending checkpoint",err)}
 catalog:=(SQLNativeParentArtifactCatalogFactory{API:p.Reader.DB,Bucket:p.Reader.Bucket}).ForReference
 person:=SQLNativePersonAdmissionRecovery{SQLNativeInitialPipelineRecovery:SQLNativeInitialPipelineRecovery{Preparer:p,CatalogForReference:catalog,Timeout:time.Minute}}
 if admitted,locked,err:=person.Recover(ctx,who,tenant,request.RunID);err!=nil||!locked||!admitted {t.Fatal("legacy person transport recovery",admitted,locked,err)}
 // Restoration requires the actual source-version catalog, never a nil or
 // permissive catalog. Resolve it through the existing recovery adapter.
 resolved,err:=resolveNativeParentRecoveryCatalog(ctx,who,anchor,p.Reader.ProjectID,nil,catalog);if err!=nil {t.Fatal(err)}
 plan,err:=p.Reader.restorePersonDraftPlan(ctx,resolved,who,anchor);if err!=nil {t.Fatal(err)}
 if plan.prepared.continuation!="ordinary_parent"||plan.prepared.ordinaryPolicy==nil||plan.prepared.ordinaryPolicy.Scene!=nil {t.Fatal("legacy frozen policy altered")}
 frozen:=append([]byte(nil),plan.shell.Config...)
 task,err:=plan.prepared.catalog.frozen.DraftTask();if err!=nil {t.Fatal(err)}
 probeNativeWireTaskOutputs(t,ctx,task.ID)
 ordinary:=SQLOrdinaryParentRecovery{SQLNativePersonAdmissionRecovery:person}
 if done,locked,err:=ordinary.Recover(ctx,who,tenant,request.RunID);err!=nil||!locked||!done {t.Fatal("legacy unsealed ordinary final recovery",done,locked,err)}
 again,err:=p.Reader.restorePersonDraftPlan(ctx,resolved,who,anchor);if err!=nil||string(again.shell.Config)!=string(frozen) {t.Fatal("legacy immutable config changed",err)}
 reply,found,err:=replayCreateReceipt(ctx,p.Reader.DB,who,request,key)
 var ticket v2input.JobTicket
 if err!=nil||!found||reply.Status!=202||json.Unmarshal(reply.Body,&ticket)!=nil||ticket.RunID!=request.RunID||ticket.WorkflowID!=request.WorkflowID||ticket.State!="queued" {t.Fatal("legacy final ordinary receipt",reply,err)}
 located,locatedKey,found,err:=p.Reader.readOrdinaryParentReceiptRequest(ctx,who,tenant,request.RunID)
 if err!=nil||!found||locatedKey!=key||located.RequestSHA!=request.RequestSHA {t.Fatal("legacy accepted locator",found,err)}
 t.Log("V27_LEGACY_ORDINARY_UNSEALED_RECOVERY_OK")
}
