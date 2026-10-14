package interchange
import("context";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/domain")
// MaterializeTask creates only a blocked task with an output quota reservation.
// Transport receipts originate in this owned ParseTransport; SQL repeats admission.
func(t ParseTransport)MaterializeTask(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant uuid.UUID,request CreateRequest,key string)error{
 if err:=ctx.Err();err!=nil{return err}
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return err}
 p:=t.plan
 if tx==nil||p.tenant!=tenant||p.identity!=intent.Identity||intent.Request.Bundle==nil||p.params.Request.Bundle!=*intent.Request.Bundle||p.params.Request.ProjectID!=request.ProjectID{return domain.NewError("VALIDATION_FAILED",422)}
 draft,err:=t.DraftTask();if err!=nil{return err};body,err:=canonical.Marshal(draft);if err!=nil{return err}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.materialize_v30_interchange_parse($1,$2,$3,$4,$5,$6,$7::jsonb)`,tenant,who.UserID,who.SessionID,key,intent.RequestSHA,string(p.paramsRaw),string(body)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("INTERCHANGE_CREATE_STALE",409)};return nil
}
