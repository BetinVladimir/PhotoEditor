package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type ImportTaskMaterializer interface {
 MaterializeImport(context.Context,ImportPreparedDocuments,v2input.DraftTask)error
}
type SQLImportTaskMaterializer struct{DB *pgxpool.Pool;Tenant uuid.UUID;Parsed []byte}
func validateImportTask(documents ImportPreparedDocuments,task v2input.DraftTask)error{
 if task.ID!=documents.TaskID()||task.NodeKey!="v2.interchange.import:0"||task.Operation!="v2.interchange.import"||task.OperationVersion!="1"||task.ResourceClass!="cpu"||task.ParamsArtifactID!=documents.params.Ref.ArtifactID||task.ParamsSHA!=documents.params.Ref.SHA256{return objectstore.ErrInvalid}
 if len(task.InputDigest)!=64{return objectstore.ErrInvalid}
 for _,c:=range task.InputDigest{if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return objectstore.ErrInvalid}}
 if len(task.ExecutionProfile)<2||len(task.ExecutionProfile)>65536{return objectstore.ErrInvalid}
 normalized,err:=canonical.Bytes(task.ExecutionProfile);if err!=nil||!bytes.Equal(normalized,task.ExecutionProfile){return objectstore.ErrInvalid}
 var profile struct{Backend string `json:"backend"`;Renderer string `json:"renderer_version"`;Estimate struct{RAM int64 `json:"ram_bytes"`;VRAM int64 `json:"vram_bytes"`;Scratch int64 `json:"scratch_bytes"`;Output int64 `json:"output_bytes"`} `json:"resource_estimate"`}
 if err:=json.Unmarshal(normalized,&profile);err!=nil||profile.Backend!="cpu"||profile.Renderer!="v2.2"||profile.Estimate.VRAM!=0||profile.Estimate.RAM<1<<30||profile.Estimate.Scratch<0||profile.Estimate.Output<1||profile.Estimate.Output>9007199254740991{return objectstore.ErrInvalid}
 for _,doc:=range documents.Documents(){if err:=validateImportStorageDocument(documents.identity,doc);err!=nil{return err}}
 return nil
}
// PrepareImportTask persists documents then the blocked shell. It deliberately
// does not return an accepted job or activate an incomplete input closure.
func PrepareImportTask(ctx context.Context,documents ImportPreparedDocuments,task v2input.DraftTask,publisher ImportDocumentPublisher,materializer ImportTaskMaterializer)error{
 if err:=ctx.Err();err!=nil{return err}
 if publisher==nil||materializer==nil{return objectstore.ErrInvalid}
 task.ExecutionProfile=append(json.RawMessage(nil),task.ExecutionProfile...)
 if err:=validateImportTask(documents,task);err!=nil{return err}
 if _,err:=documents.Publish(ctx,publisher);err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 return materializer.MaterializeImport(ctx,documents,task)
}
func(s SQLImportTaskMaterializer)MaterializeImport(ctx context.Context,documents ImportPreparedDocuments,task v2input.DraftTask)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.Tenant==uuid.Nil||len(s.Parsed)<2||len(s.Parsed)>8<<20{return objectstore.ErrInvalid}
 if err:=validateImportTask(documents,task);err!=nil{return err}
 parsed:=append([]byte(nil),s.Parsed...)
 normalized,err:=canonical.Bytes(parsed);if err!=nil||!bytes.Equal(normalized,parsed){return objectstore.ErrInvalid}
 var params struct{Parsed v2input.Ref `json:"parsed"`}
 if err:=json.Unmarshal(documents.params.Body,&params);err!=nil||params.Parsed.SHA256!=rawSHA(parsed){return objectstore.ErrIntegrity}
 raw,err:=canonical.Marshal(task);if err!=nil{return err}
 return database.TenantTransaction(ctx,s.DB,s.Tenant,func(tx pgx.Tx)error{
  var run uuid.UUID
  if err:=tx.QueryRow(ctx,`SELECT run_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`,s.Tenant,documents.identity.Workflow).Scan(&run);err!=nil{return err}
  if run!=documents.identity.Run{return objectstore.ErrIntegrity}
  var accepted bool
  if err:=tx.QueryRow(ctx,`SELECT photo.materialize_v30_interchange_import($1,$2,$3,$4,$5,$6::jsonb)`,s.Tenant,documents.identity.Workflow,string(parsed),string(documents.plan.Body),string(documents.params.Body),string(raw)).Scan(&accepted);err!=nil{return err}
  if !accepted{return artifactjson.ErrStale};return nil
 })
}
