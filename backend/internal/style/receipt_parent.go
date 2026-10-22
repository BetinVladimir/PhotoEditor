package style

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

func bindReceiptParent(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, route,key string, out idempotency.Response, parent string, proof string, args ...any) error {
 return idempotency.BindParent(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:route,Key:key},out,parent,func() error {
  if proof=="" || out.ResourceID==nil { return domain.NewError("IDEMPOTENCY_CONFLICT",409) }
  values:=[]any{tid,*out.ResourceID};values=append(values,args...)
  var bound bool
  if err:=tx.QueryRow(ctx,proof,values...).Scan(&bound);err!=nil{return err}
  if !bound{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  return nil
 })
}
