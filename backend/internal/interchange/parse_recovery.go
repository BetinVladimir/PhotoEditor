package interchange
import(
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ParseRecovery struct{Plan ParsePlan;HasPending bool;Completed bool;Response idempotency.Response}
// RecoverParse performs no object IO and requires current project access even
// for completed tickets. It never turns private pending evidence into success.
func RecoverParse(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(ParseRecovery,error){
 if err:=ctx.Err();err!=nil{return ParseRecovery{},err}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return ParseRecovery{},err}
 if tx==nil{return ParseRecovery{},domain.NewError("VALIDATION_FAILED",422)}
 if request.Direction!="import"||!supportedParseFormat(request.Format){return ParseRecovery{},domain.NewError("INTERCHANGE_FORMAT_UNAVAILABLE",503)}
 var allowed bool
 err=tx.QueryRow(ctx,`SELECT photo.v2_editor_authorized($1,$2,$3,$4) AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$2 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active')`,tid,request.ProjectID,who.UserID,who.SessionID).Scan(&allowed)
 if err!=nil{return ParseRecovery{},err};if !allowed{return ParseRecovery{},domain.NewError("NOT_FOUND",404)}
 var hash string;var body []byte;var live bool;var status *int;var resource *uuid.UUID
 err=tx.QueryRow(ctx,`SELECT request_hash,response_body,expires_at>clock_timestamp(),response_status,resource_id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Interchange.Parse.Ticket' AND key=$3 FOR SHARE`,tid,who.UserID,key).Scan(&hash,&body,&live,&status,&resource)
 if err==nil{
  if hash!=intent.RequestSHA{return ParseRecovery{},domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  if !live{return ParseRecovery{},domain.NewError("SNAPSHOT_EXPIRED",410)}
  if status==nil||*status!=202||resource==nil||*resource!=intent.Identity.Workflow{return ParseRecovery{},domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
  var committed bool
  err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id=$6 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=r.id AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready' WHERE r.tenant_id=$1 AND r.id=$2 AND r.kind='v2' AND r.graph_sealed AND r.status<>'draft' AND r.created_by=$3 AND r.config_snapshot->>'request_sha'=$4 AND w.id=$5 AND w.project_id=$7 AND w.input_snapshot_id=$8 AND w.workflow_type='interchange' AND w.policy_sha=$4 AND t.operation='v2.interchange.parse' AND t.operation_version='1' AND t.params_artifact_id=$9 AND d.v30_interchange_proof->>'project_id'=$7::text AND d.v30_interchange_proof->'bundle'=jsonb_build_object('artifact_id',$10::uuid,'sha256',$11::text))`,tid,intent.Identity.Run,who.UserID,intent.RequestSHA,intent.Identity.Workflow,intent.Identity.Task,request.ProjectID,intent.Identity.Snapshot,intent.Identity.Params,request.Bundle.ArtifactID,request.Bundle.SHA256).Scan(&committed)
  if err!=nil{return ParseRecovery{},err};if !committed{return ParseRecovery{},domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
  normalized,err:=json.Marshal(struct{Ticket json.RawMessage `json:"ticket"`;Replayed bool `json:"replayed"`}{Ticket:body,Replayed:true});if err!=nil{return ParseRecovery{},v2input.ErrInvalid}
  response,err:=decodeParseTicket(normalized,intent.Identity);if err!=nil{return ParseRecovery{},err}
  return ParseRecovery{Completed:true,Response:response},nil
 }
 if !errors.Is(err,pgx.ErrNoRows){return ParseRecovery{},err}
 err=tx.QueryRow(ctx,`SELECT request_hash,response_body,expires_at>clock_timestamp(),response_status FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Interchange.Parse.Pending' AND key=$3 FOR SHARE`,tid,who.UserID,key).Scan(&hash,&body,&live,&status)
 if errors.Is(err,pgx.ErrNoRows){return ParseRecovery{},nil};if err!=nil{return ParseRecovery{},err}
 if hash!=intent.RequestSHA{return ParseRecovery{},domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 if !live{return ParseRecovery{},domain.NewError("SNAPSHOT_EXPIRED",410)}
 if status!=nil{return ParseRecovery{},domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
 if len(body)==0||bytes.Equal(bytes.TrimSpace(body),[]byte("null")){return ParseRecovery{},nil}
 plan,err:=DecodeParsePending(tid,who,intent.Request,key,body);if err!=nil{return ParseRecovery{},err}
 evidence,err:=ResolveLinkedBundle(ctx,tx,who,tid,intent.Request);if err!=nil{return ParseRecovery{},err}
 if evidence.Bytes!=plan.archiveBytes{return ParseRecovery{},domain.NewError("INTERCHANGE_SOURCE_STALE",409)}
 return ParseRecovery{Plan:plan,HasPending:true},nil
}
func(s Service)RecoverParse(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(ParseRecovery,error){
 if s.DB==nil{return ParseRecovery{},domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 var result ParseRecovery
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{var err error;result,err=RecoverParse(ctx,tx,who,tid,request,key);return err})
 return result,err
}
