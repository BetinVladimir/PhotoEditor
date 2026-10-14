package v2optical

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type NegativeReplacementPublicationIdentity struct{
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 OriginalTask uuid.UUID `json:"original_task_id"`
 OriginalRun uuid.UUID `json:"original_run_id"`
 Run uuid.UUID `json:"run_id"`
 Workflow uuid.UUID `json:"workflow_id"`
 Task uuid.UUID `json:"task_id"`
 ParamsDocument uuid.UUID `json:"params_document_id"`
 Operation string `json:"operation"`
 ParamsSHA string `json:"params_sha"`
 MarkerSHA string `json:"marker_sha"`
}

// PublicationIdentity freezes retry identities around exact mapped Params and
// run marker. This is not an admission/draft/queue receipt; the dedicated SQL
// publisher must recheck original context, physical proofs and qualification.
// Replacement task IDs deliberately differ from ordinary operation ordinal IDs.
func(p PreparedMappedReplacementStep)PublicationIdentity(ctx context.Context,lineRef *v2input.Ref)(NegativeReplacementPublicationIdentity,string,error){
 var zero NegativeReplacementPublicationIdentity
 _,markerSHA,err:=p.RunMarker(ctx);if err!=nil{return zero,"",err}
 intent,err:=p.Rebind(ctx,lineRef);if err!=nil{return zero,"",err}
 n:=p.Node();l:=p.parents.step.intent.locator
 workflow,err:=v2input.WorkflowID(n.Run);if err!=nil{return zero,"",err}
 out:=NegativeReplacementPublicationIdentity{SchemaVersion:2,Tenant:l.Tenant,Project:l.Project,Review:l.Review,DecisionHash:l.Hash,OriginalTask:n.OriginalTask,OriginalRun:n.OriginalRun,Run:n.Run,Workflow:workflow,Task:n.Task,ParamsDocument:uuid.NewSHA1(n.Task,[]byte("params")),Operation:intent.Operation(),ParamsSHA:intent.SHA256(),MarkerSHA:markerSHA}
 digest,err:=canonical.Hash(out);if err!=nil{return zero,"",err};return out,digest,ctx.Err()
}
