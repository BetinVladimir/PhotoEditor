package interchange

import (
 "bytes"
 "context"
 "errors"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
)

type PendingTransaction interface {
 ProofTransaction
 Exec(context.Context,string,...any)(pgconn.CommandTag,error)
}
type Service struct {DB *pgxpool.Pool;ApprovalStyles ImportStyleQualifier;ApprovalStyleFactory ImportReviewStyleQualifierFactory}

// ResolveLinkedBundle accepts only physically ready ZIP bytes already linked by
// server adoption to this project. It does not authorize an unlinked upload or
// infer project access from a guessed UUID. Caller must hold current editor ACL.
func ResolveLinkedBundle(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,request CreateRequest)(BundleEvidence,error){
 if err:=ctx.Err();err!=nil{return BundleEvidence{},err}
 if tx==nil||tid==uuid.Nil||request.Direction!="import"||request.Bundle==nil{return BundleEvidence{},domain.NewError("VALIDATION_FAILED",422)}
 var size int64
 err:=tx.QueryRow(ctx,`SELECT a.bytes FROM photo.artifacts a WHERE a.tenant_id=$1 AND a.id=$3 AND a.sha256=$4
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.media_type='application/zip' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v30_import_archive_ready($1,$2,$5,$6,a.id,a.sha256) FOR SHARE OF a`,tid,request.ProjectID,request.Bundle.ArtifactID,request.Bundle.SHA256,who.UserID,who.SessionID).Scan(&size)
 if errors.Is(err,pgx.ErrNoRows){return BundleEvidence{},domain.NewError("NOT_FOUND",404)}
 if err!=nil{return BundleEvidence{},err}
 if size<22||size>64<<30{return BundleEvidence{},domain.NewError("VALIDATION_FAILED",422)}
 return BundleEvidence{Ref:*request.Bundle,Bytes:size},nil
}

// PinParsePending locks the actor/key ledger before pinning bytes/resources.
// Concurrent callers recover the original resource limits, never a new plan.
// Object IO and run/task construction belong after this transaction commits.
func PinParsePending(ctx context.Context,tx PendingTransaction,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,limits ParseLimits)(ParsePlan,error){
 if err:=ctx.Err();err!=nil{return ParsePlan{},err}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return ParsePlan{},err}
 if tx==nil{return ParsePlan{},domain.NewError("VALIDATION_FAILED",422)}
 request=intent.Request
 if request.Direction!="import"||!supportedParseFormat(request.Format){return ParsePlan{},domain.NewError("INTERCHANGE_FORMAT_UNAVAILABLE",503)}
 var allowed bool
 if err:=tx.QueryRow(ctx,`SELECT photo.v2_editor_authorized($1,$2,$3,$4) AND EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND download_allowed)`,tid,request.ProjectID,who.UserID,who.SessionID).Scan(&allowed);err!=nil{return ParsePlan{},err}
 if !allowed{return ParsePlan{},domain.NewError("NOT_FOUND",404)}
 const route="Interchange.Parse.Pending"
 if _,err:=tx.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,$3,$4,$5,clock_timestamp()+interval '24 hours') ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING`,tid,who.UserID,route,key,intent.RequestSHA);err!=nil{return ParsePlan{},err}
 var hash string;var body []byte;var live bool;var status *int
 if err:=tx.QueryRow(ctx,`SELECT request_hash,response_body,expires_at>clock_timestamp(),response_status FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 FOR UPDATE`,tid,who.UserID,route,key).Scan(&hash,&body,&live,&status);err!=nil{return ParsePlan{},err}
 if err:=requireReceiptAuthority(ctx,tx,who,tid,request.ProjectID);err!=nil{return ParsePlan{},err}
 if hash!=intent.RequestSHA{return ParsePlan{},domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 if !live{return ParsePlan{},domain.NewError("SNAPSHOT_EXPIRED",410)}
 if status!=nil{return ParsePlan{},domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
 evidence,err:=ResolveLinkedBundle(ctx,tx,who,tid,request);if err!=nil{return ParsePlan{},err}
 if len(body)>0&&!bytes.Equal(bytes.TrimSpace(body),[]byte("null")){
  plan,err:=DecodeParsePending(tid,who,request,key,body);if err!=nil{return ParsePlan{},err}
  if plan.archiveBytes!=evidence.Bytes{return ParsePlan{},domain.NewError("INTERCHANGE_SOURCE_STALE",409)}
  return plan,nil
 }
 body,err=EncodeParsePending(tid,who,request,key,evidence,limits);if err!=nil{return ParsePlan{},err}
 plan,err:=DecodeParsePending(tid,who,request,key,body);if err!=nil{return ParsePlan{},err}
 tag,err:=tx.Exec(ctx,`UPDATE photo.idempotency_records SET response_body=$5::jsonb WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 AND response_status IS NULL`,tid,who.UserID,route,key,string(body));if err!=nil{return ParsePlan{},err}
 if tag.RowsAffected()!=1{return ParsePlan{},domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
 return plan,nil
}
func(s Service)PinParsePending(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,limits ParseLimits)(ParsePlan,error){
 if s.DB==nil{return ParsePlan{},domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 var plan ParsePlan
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var err error;plan,err=PinParsePending(ctx,tx,who,tid,request,key,limits);return err
 })
 return plan,err
}
