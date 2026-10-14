package interchange
import(
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
)
// ActivateParse returns only an atomically committed ticket or its authorized
// replay. The private pending plan remains intact for source/intent recovery.
func(p ParsePlan)ActivateParse(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant uuid.UUID,request CreateRequest,key string)(idempotency.Response,error){
 if err:=ctx.Err();err!=nil{return idempotency.Response{},err}
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return idempotency.Response{},err}
 if tx==nil||p.tenant!=tenant||p.identity!=intent.Identity||intent.Request.Bundle==nil||p.params.Request.Bundle!=*intent.Request.Bundle||p.params.Request.ProjectID!=request.ProjectID{return idempotency.Response{},domain.NewError("VALIDATION_FAILED",422)}
 var raw []byte
 err=tx.QueryRow(ctx,`SELECT photo.activate_v30_interchange_parse($1,$2,$3,$4,$5,$6)`,tenant,who.UserID,who.SessionID,key,intent.RequestSHA,string(p.paramsRaw)).Scan(&raw)
 if err!=nil{
  var refusal *pgconn.PgError
  if errors.As(err,&refusal)&&refusal.Code=="23514"&&refusal.Message=="INTERCHANGE_INITIAL_ACTIVATION_REFUSED"{return idempotency.Response{},domain.NewError("INTERCHANGE_CREATE_STALE",409)}
  return idempotency.Response{},err
 }
 return decodeParseTicket(raw,p.identity)
}
func decodeParseTicket(raw []byte,identity CreateIdentity)(idempotency.Response,error){
 if len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return idempotency.Response{},domain.NewError("INTERCHANGE_CREATE_STALE",409)}
 if len(raw)>4096{return idempotency.Response{},v2input.ErrInvalid}
 var receipt struct{Ticket v2input.JobTicket `json:"ticket"`;Replayed *bool `json:"replayed"`}
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&receipt)!=nil||decoder.Decode(new(any))!=io.EOF||receipt.Replayed==nil||receipt.Ticket.RunID!=identity.Run||receipt.Ticket.WorkflowID!=identity.Workflow||receipt.Ticket.State!="queued"{return idempotency.Response{},v2input.ErrInvalid}
 body,err:=canonical.Marshal(receipt.Ticket);if err!=nil{return idempotency.Response{},err}
 id:=identity.Workflow
 return idempotency.Response{Status:202,Body:body,ResourceID:&id,Replayed:*receipt.Replayed},nil
}
