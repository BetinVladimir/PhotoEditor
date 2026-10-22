package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
// Pending records are private recovery data, never public creation tickets.
type exportPendingPayload struct{Schema int `json:"schema_version"`;Tenant uuid.UUID `json:"tenant_id"`;Actor uuid.UUID `json:"actor_id"`;Frozen json.RawMessage `json:"frozen"`}
func PinExportPending(ctx context.Context,tx PendingTransaction,who auth.Identity,tenant uuid.UUID,request CreateRequest,key string,frozen FrozenExportDocuments)(FrozenExportDocuments,CompiledExportPlan,error){
 var zero FrozenExportDocuments;var plan CompiledExportPlan
 if err:=ctx.Err();err!=nil{return zero,plan,err}
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return zero,plan,err}
 request=intent.Request
 if tx==nil||request.Direction!="export"||request.SnapshotID==nil{return zero,plan,v2input.ErrInvalid}
 checked,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return zero,plan,err}
 var allowed bool
 if err:=tx.QueryRow(ctx,`SELECT photo.v25_lens_snapshot_ready($1,$2,$3,$4,$5) AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$2 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active')`,tenant,request.ProjectID,*request.SnapshotID,who.UserID,who.SessionID).Scan(&allowed);err!=nil{return zero,plan,err}
 if !allowed{return zero,plan,domain.NewError("NOT_FOUND",404)}
 const route="Interchange.Export.Pending"
 if _,err:=tx.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,$3,$4,$5,clock_timestamp()+interval '24 hours') ON CONFLICT DO NOTHING`,tenant,who.UserID,route,key,intent.RequestSHA);err!=nil{return zero,plan,err}
 var hash string;var body []byte;var live bool;var status *int
 if err:=tx.QueryRow(ctx,`SELECT request_hash,response_body,expires_at>clock_timestamp(),response_status FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 FOR UPDATE`,tenant,who.UserID,route,key).Scan(&hash,&body,&live,&status);err!=nil{return zero,plan,err}
 if err:=requireReceiptAuthority(ctx,tx,who,tenant,request.ProjectID);err!=nil{return zero,plan,err}
 if hash!=intent.RequestSHA{return zero,plan,domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 if !live{return zero,plan,domain.NewError("SNAPSHOT_EXPIRED",410)}
 if status!=nil{return zero,plan,domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
 if len(body)>0&&!bytes.Equal(bytes.TrimSpace(body),[]byte("null")){
  // Existing evidence wins even if a concurrent compiler proposed new outputs.
  return decodeExportPending(ctx,intent,tenant,who.UserID,body)
 }
 body,err=canonical.Marshal(exportPendingPayload{Schema:1,Tenant:tenant,Actor:who.UserID,Frozen:checked.Body()});if err!=nil{return zero,plan,err}
 tag,err:=tx.Exec(ctx,`UPDATE photo.idempotency_records SET response_body=$5::jsonb WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 AND response_status IS NULL`,tenant,who.UserID,route,key,string(body));if err!=nil{return zero,plan,err}
 if tag.RowsAffected()!=1{return zero,plan,domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
 return checked,compiled,ctx.Err()
}

func decodeExportPending(ctx context.Context,intent CreateIntent,tenant,actor uuid.UUID,body []byte)(FrozenExportDocuments,CompiledExportPlan,error){
 var zero FrozenExportDocuments;var plan CompiledExportPlan
 if len(body)<2||len(body)>25<<20{return zero,plan,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(body);if err!=nil{return zero,plan,v2input.ErrInvalid}
 var saved exportPendingPayload
 decoder:=json.NewDecoder(bytes.NewReader(normalized));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&saved);err!=nil{return zero,plan,err}
 exact,err:=canonical.Marshal(saved)
 if err!=nil||!bytes.Equal(exact,normalized)||saved.Schema!=1||saved.Tenant!=tenant||saved.Actor!=actor{return zero,plan,v2input.ErrInvalid}
 return DecodeFrozenExportDocuments(ctx,intent,saved.Frozen)
}
