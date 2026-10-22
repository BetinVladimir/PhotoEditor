package semanticproposal

import (
 "context"
 "bytes"
 "io"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// replayCreateReceipt recovers only an already committed admission. A missing or
// unfinished admission must still traverse the complete existing preparation and
// sealing boundary. No producer, snapshot refresh or new receipt is invoked.
func replayCreateReceipt(ctx context.Context,db *pgxpool.Pool,who auth.Identity,request Request,key string)(idempotency.Response,bool,error){
 var out idempotency.Response;var found bool
 if err:=ctx.Err();err!=nil{return out,false,err}
 if db==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||who.UserID!=request.ActorID{return out,false,v2input.ErrInvalid}
 err:=tenant.WithTenant(ctx,db,who,request.TenantID,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var hash string;var status *int;var body []byte;var resource *uuid.UUID
  e:=tx.QueryRow(ctx,`SELECT request_hash,response_status,response_body,resource_id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='V2.Admit' AND key=$3`,request.TenantID,who.UserID,key).Scan(&hash,&status,&body,&resource)
  if errors.Is(e,pgx.ErrNoRows){return nil};if e!=nil{return e};if hash!=request.RequestSHA{return v2input.ErrConflict}
  if status==nil||*status!=202||resource==nil{return nil}
  if *resource!=request.RunID{return v2input.ErrConflict}
  var kind,configHash string;var config []byte;var sealed bool;var creator,workflow,projectID,snapshotID uuid.UUID;var workflowType,renderer string
  e=tx.QueryRow(ctx,`SELECT r.kind,r.graph_sealed,r.created_by,r.config_snapshot,r.config_hash,w.id,w.project_id,w.input_snapshot_id,w.workflow_type,w.renderer_version FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2`,request.TenantID,request.RunID).Scan(&kind,&sealed,&creator,&config,&configHash,&workflow,&projectID,&snapshotID,&workflowType,&renderer)
  if e!=nil{return e};if kind!="v2"||creator!=who.UserID||workflow!=request.WorkflowID||workflowType!="semantic"||renderer!="v2.2"{return v2input.ErrConflict}
  if _,e=project.Authorize(ctx,tx,request.TenantID,projectID,who.UserID,role,domain.Editor,false);e!=nil{return e}
  // A blocked shell may legitimately precede final semantic frozen nodes.
  // Its exact receipt/resource/workflow identity is checked above, but only
  // the original complete preparation path may resume or activate it.
  if !sealed{return nil}
  var frozen v2input.SemanticDraftConfig
  if json.Unmarshal(config,&frozen)!=nil||frozen.ProjectID==uuid.Nil||frozen.ProjectID!=projectID||frozen.SnapshotID!=snapshotID||frozen.Semantic.AuthSessionID==uuid.Nil||len(frozen.Semantic.Nodes)<1{return v2input.ErrConflict}
  actualHash,e:=canonical.Hash(json.RawMessage(config));if e!=nil{return e};if actualHash!=configHash{return v2input.ErrConflict}
  var ticket v2input.JobTicket
  decoder:=json.NewDecoder(bytes.NewReader(body));decoder.DisallowUnknownFields()
  if len(body)>4096||decoder.Decode(&ticket)!=nil||decoder.Decode(new(any))!=io.EOF||ticket.RunID!=request.RunID||ticket.WorkflowID!=request.WorkflowID||ticket.State!="queued"{return v2input.ErrConflict}
  // Preserve the committed response bytes. A fresh command cannot manufacture
  // this receipt, even when a deterministic run happens to exist.
  out=idempotency.Response{Status:202,Body:append([]byte(nil),body...),ResourceID:resource};found=true;return nil
 })
 if err!=nil{return idempotency.Response{},false,err};if err=ctx.Err();err!=nil{return idempotency.Response{},false,err};return out,found,nil
}
