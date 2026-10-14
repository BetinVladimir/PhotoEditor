package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// BeginParseRun creates draft-only DB containers in caller's tenant transaction.
// The private pending ledger remains the authoritative source; SQL checks it.
func(p ParsePlan)BeginParseRun(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant uuid.UUID,request CreateRequest,key string)error{
 if err:=ctx.Err();err!=nil{return err}
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return err}
 if (p.queueClass!=""&&p.queueClass!="batch")||tx==nil||p.tenant!=tenant||p.identity!=intent.Identity||intent.Request.Bundle==nil||p.params.Request.Bundle!=*intent.Request.Bundle||p.params.Request.ProjectID!=request.ProjectID{return domain.NewError("VALIDATION_FAILED",422)}
 document,err:=p.CompileSnapshot();if err!=nil{return err}
 unsigned,err:=canonical.Marshal(struct{
  SchemaVersion int `json:"schema_version"`;SnapshotID uuid.UUID `json:"snapshot_id"`;ProjectID uuid.UUID `json:"project_id"`;Items []ArtifactItem `json:"items"`
 }{1,document.Snapshot.SnapshotID,document.Snapshot.ProjectID,document.Snapshot.Items});if err!=nil{return err}
 config,err:=canonical.Marshal(struct{
  QueueClass string `json:"queue_class,omitempty"`;
  SchemaVersion int `json:"schema_version"`;ProjectID uuid.UUID `json:"project_id"`;SnapshotID uuid.UUID `json:"snapshot_id"`;Workflow string `json:"workflow_type"`;RequestSHA string `json:"request_sha"`;SnapshotDigest string `json:"snapshot_digest"`;SnapshotDocument v2input.Ref `json:"snapshot_document"`
 }{p.queueClass,1,request.ProjectID,p.identity.Snapshot,"interchange",intent.RequestSHA,document.Snapshot.SnapshotDigest,document.Ref});if err!=nil{return err}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.begin_v30_interchange_parse_run($1,$2,$3,$4,$5,$6,$7,$8)`,tenant,who.UserID,who.SessionID,key,intent.RequestSHA,string(config),string(document.Body),string(unsigned)).Scan(&accepted)
 if err!=nil{return err};if !accepted{
  var runtimeReady bool
  if err=tx.QueryRow(ctx,`SELECT photo.v30_import_runtime_ready()`).Scan(&runtimeReady);err!=nil{return err}
  if !runtimeReady{return domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
  return domain.NewError("INTERCHANGE_CREATE_STALE",409)
 };return nil
}
