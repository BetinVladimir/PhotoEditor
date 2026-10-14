package interchange

import (
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

// RecoverExportTicket replays only the committed creation receipt, independently
// of pending-ledger cleanup or input expiration. Current project download access
// remains mandatory. It performs no archive IO and does not assert completion.
func RecoverExportTicket(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(idempotency.Response,bool,error){
 var zero idempotency.Response
 if err:=ctx.Err();err!=nil{return zero,false,err}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return zero,false,err}
 if tx==nil||request.Direction!="export"{return zero,false,v2input.ErrInvalid}
 var allowed bool
 err=tx.QueryRow(ctx,`SELECT photo.v2_editor_authorized($1,$2,$3,$4) AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$2 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active')`,tid,request.ProjectID,who.UserID,who.SessionID).Scan(&allowed)
 if err!=nil{return zero,false,err};if !allowed{return zero,false,domain.NewError("NOT_FOUND",404)}
 var hash string;var body []byte;var live bool;var status *int;var resource *uuid.UUID
 err=tx.QueryRow(ctx,`SELECT request_hash,response_body,expires_at>clock_timestamp(),response_status,resource_id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Interchange.Export.Ticket' AND key=$3 FOR SHARE`,tid,who.UserID,key).Scan(&hash,&body,&live,&status,&resource)
 if errors.Is(err,pgx.ErrNoRows){return zero,false,nil};if err!=nil{return zero,false,err}
 if hash!=intent.RequestSHA{return zero,false,domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 if !live{return zero,false,domain.NewError("SNAPSHOT_EXPIRED",410)}
 if status==nil||*status!=202||resource==nil||*resource!=intent.Identity.Workflow||len(body)>4096{return zero,false,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,false,err}
 var committed bool
 err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id=$6 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=r.id AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready' WHERE r.tenant_id=$1 AND r.id=$2 AND r.kind='v2' AND r.graph_sealed AND r.status<>'draft' AND r.created_by=$3 AND r.config_snapshot->>'request_sha'=$4 AND w.id=$5 AND w.project_id=$7 AND w.input_snapshot_id=$8 AND w.workflow_type='interchange' AND w.renderer_version='v2.2' AND t.operation='v2.interchange.export' AND t.operation_version='1' AND d.v30_interchange_proof->>'direction'='export' AND d.v30_interchange_proof->>'actor_id'=$3::text AND d.v30_interchange_proof->>'request_sha'=$4 AND d.v30_interchange_proof->>'key'=$9)`,tid,intent.Identity.Run,who.UserID,intent.RequestSHA,intent.Identity.Workflow,intent.Identity.Task,request.ProjectID,intent.Identity.Snapshot,key).Scan(&committed)
 if err!=nil{return zero,false,err};if !committed{return zero,false,v2input.ErrInvalid}
 normalized,err:=json.Marshal(struct{Ticket json.RawMessage `json:"ticket"`;Replayed bool `json:"replayed"`}{Ticket:body,Replayed:true});if err!=nil{return zero,false,v2input.ErrInvalid}
 response,err:=decodeParseTicket(normalized,intent.Identity);if err!=nil{return zero,false,err}
 if err:=ctx.Err();err!=nil{return zero,false,err}
 return response,true,nil
}
func(s Service)RecoverExportTicket(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(idempotency.Response,bool,error){
 if s.DB==nil{return idempotency.Response{},false,domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 var response idempotency.Response;var found bool
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{var err error;response,found,err=RecoverExportTicket(ctx,tx,who,tid,request,key);return err})
 if err!=nil{return idempotency.Response{},false,err};return response,found,nil
}
