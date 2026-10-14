package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// AttachSnapshot retains the verified root in the caller's tenant transaction.
// SQL independently binds published bytes to the pending intent and draft run.
func(p ParsePlan)AttachSnapshot(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant uuid.UUID,request CreateRequest,key string)error{
 if err:=ctx.Err();err!=nil{return err}
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return err}
 if tx==nil||p.tenant!=tenant||p.identity!=intent.Identity||intent.Request.Bundle==nil||p.params.Request.Bundle!=*intent.Request.Bundle||p.params.Request.ProjectID!=request.ProjectID{return domain.NewError("VALIDATION_FAILED",422)}
 root,err:=p.CompileSnapshot();if err!=nil{return err}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.attach_v30_interchange_snapshot($1,$2,$3,$4,$5,$6,$7)`,tenant,who.UserID,who.SessionID,key,intent.RequestSHA,root.Ref.ArtifactID,string(root.Body)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("INTERCHANGE_CREATE_STALE",409)};return nil
}
