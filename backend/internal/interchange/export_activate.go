package interchange

import (
 "bytes"
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// Activate must run in a caller-owned transaction which rolls back on any error.
// Only Prepare can construct the owned receipt on the production path.
func(p PreparedExportActivation)Activate(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(idempotency.Response,error){
 if err:=ctx.Err();err!=nil{return idempotency.Response{},err}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return idempotency.Response{},err}
 if tx==nil||p.intent.Identity!=intent.Identity||p.intent.RequestSHA!=intent.RequestSHA||p.task.ID!=intent.Identity.Task||p.inventory.task!=p.task.ID||p.task.InputDigest!=p.inventory.digest||len(p.frozen.body)==0{return idempotency.Response{},v2input.ErrInvalid}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,p.frozen.Body());err!=nil{return idempotency.Response{},err}
 digest,err:=canonical.Hash(p.inventory.entries);if err!=nil||digest!=p.inventory.digest{return idempotency.Response{},v2input.ErrInvalid}
 body,err:=canonical.Marshal(p.task);if err!=nil{return idempotency.Response{},err}
 var raw []byte
 err=tx.QueryRow(ctx,`SELECT photo.activate_v30_interchange_export($1,$2,$3,$4,$5,$6::jsonb)`,tid,who.UserID,who.SessionID,key,intent.RequestSHA,string(body)).Scan(&raw)
 if err!=nil{
  var refusal *pgconn.PgError
  if errors.As(err,&refusal)&&refusal.Code=="23514"&&refusal.Message=="INTERCHANGE_EXPORT_ACTIVATION_REFUSED"{return idempotency.Response{},domain.NewError("INTERCHANGE_CREATE_STALE",409)}
  return idempotency.Response{},err
 }
 if err:=ctx.Err();err!=nil{return idempotency.Response{},err}
 if len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){
  var runtime *bool
  if diagnosisErr:=tx.QueryRow(ctx,`SELECT photo.v30_export_pending_runtime_ready($1,$2,$3,$4,$5)`,
   tid,who.UserID,who.SessionID,key,intent.RequestSHA).Scan(&runtime);diagnosisErr!=nil{return idempotency.Response{},diagnosisErr}
  if runtime!=nil&&!*runtime{return idempotency.Response{},domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 }
 return decodeParseTicket(raw,intent.Identity)
}
func(s Service)ActivateExport(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,prepared PreparedExportActivation)(idempotency.Response,error){
 if s.DB==nil{return idempotency.Response{},v2input.ErrInvalid}
 var result idempotency.Response
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{var err error;result,err=prepared.Activate(ctx,tx,who,tid,request,key);return err})
 if err!=nil{return idempotency.Response{},err};return result,nil
}
