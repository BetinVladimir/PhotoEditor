package collection

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

func bindCollectionReceipt(ctx context.Context,tx pgx.Tx,who auth.Identity,tid,collectionID uuid.UUID,route,key string,out idempotency.Response,proof string) error {
 return idempotency.BindParent(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:route,Key:key},out,"collection:"+collectionID.String(),func()error{
  if out.ResourceID==nil{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  var bound bool
  if err:=tx.QueryRow(ctx,proof,tid,*out.ResourceID,collectionID).Scan(&bound);err!=nil{return err}
  if !bound{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  return nil
 })
}
