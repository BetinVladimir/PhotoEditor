package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

func exportDraftConfig(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments)([]byte,error){
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return nil,err}
 var payload exportFreezePayload
 // Recovery has already checked canonical encoding, identity, hashes and bindings.
 if err:=json.Unmarshal(checked.Body(),&payload);err!=nil{return nil,err}
 var snapshot v2input.SessionInputSnapshot
 if err:=decodeImportDocument([]byte(payload.SnapshotJCS),&snapshot);err!=nil{return nil,err}
 return canonical.Marshal(struct{
  QueueClass string `json:"queue_class,omitempty"`;
  Schema int `json:"schema_version"`;Project uuid.UUID `json:"project_id"`;Snapshot uuid.UUID `json:"snapshot_id"`
  Workflow string `json:"workflow_type"`;RequestSHA string `json:"request_sha"`;Digest string `json:"snapshot_digest"`;Document v2input.Ref `json:"snapshot_document"`
 }{payload.QueueClass,1,intent.Request.ProjectID,intent.Identity.Snapshot,"interchange",intent.RequestSHA,snapshot.SnapshotDigest,payload.Snapshot})
}

// BeginExportRun creates only draft containers. SQL rechecks the pending ledger
// and current snapshot authority. Publication and graph sealing are separate.
func(f FrozenExportDocuments)BeginExportRun(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant uuid.UUID,request CreateRequest,key string)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil{return domain.NewError("VALIDATION_FAILED",422)}
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return err}
 config,err:=exportDraftConfig(ctx,intent,f);if err!=nil{return err}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.begin_v30_interchange_export_run($1,$2,$3,$4,$5,$6)`,tenant,who.UserID,who.SessionID,key,intent.RequestSHA,string(config)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("INTERCHANGE_CREATE_STALE",409)};return nil
}
