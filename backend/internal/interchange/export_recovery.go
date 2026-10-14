package interchange
import (
 "bytes"
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ExportPendingRecovery struct{Frozen FrozenExportDocuments;Plan CompiledExportPlan;HasPending bool}
// No resolver/object IO and no inferred completion: pending data remains private.
func RecoverExportPending(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(ExportPendingRecovery,error){
 var zero ExportPendingRecovery
 if err:=ctx.Err();err!=nil{return zero,err}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return zero,err}
 request=intent.Request
 if tx==nil||request.Direction!="export"||request.SnapshotID==nil{return zero,v2input.ErrInvalid}
 var allowed bool
 if err:=tx.QueryRow(ctx,`SELECT photo.v25_lens_snapshot_ready($1,$2,$3,$4,$5) AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$2 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active')`,tid,request.ProjectID,*request.SnapshotID,who.UserID,who.SessionID).Scan(&allowed);err!=nil{return zero,err}
 if !allowed{return zero,domain.NewError("NOT_FOUND",404)}
 var hash string;var body []byte;var live bool;var status *int
 err=tx.QueryRow(ctx,`SELECT request_hash,response_body,expires_at>clock_timestamp(),response_status FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Interchange.Export.Pending' AND key=$3 FOR SHARE`,tid,who.UserID,key).Scan(&hash,&body,&live,&status)
 if errors.Is(err,pgx.ErrNoRows){return zero,nil};if err!=nil{return zero,err}
 if hash!=intent.RequestSHA{return zero,domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 if !live{return zero,domain.NewError("SNAPSHOT_EXPIRED",410)}
 if status!=nil{return zero,domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
 if len(body)==0||bytes.Equal(bytes.TrimSpace(body),[]byte("null")){return zero,nil}
 frozen,plan,err:=decodeExportPending(ctx,intent,tid,who.UserID,body);if err!=nil{return zero,err}
 return ExportPendingRecovery{Frozen:frozen,Plan:plan,HasPending:true},ctx.Err()
}
func(s Service)RecoverExportPending(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(ExportPendingRecovery,error){
 if s.DB==nil{return ExportPendingRecovery{},domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 var result ExportPendingRecovery
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{var err error;result,err=RecoverExportPending(ctx,tx,who,tid,request,key);return err})
 return result,err
}
func(s Service)PinExportPending(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,frozen FrozenExportDocuments)(FrozenExportDocuments,CompiledExportPlan,error){
 if s.DB==nil{return FrozenExportDocuments{},CompiledExportPlan{},domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 var result FrozenExportDocuments;var plan CompiledExportPlan
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{var err error;result,plan,err=PinExportPending(ctx,tx,who,tid,request,key,frozen);return err})
 return result,plan,err
}
