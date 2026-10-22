package tenant

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// authorityTransaction owns only this transaction's preflight gates. No global
// registry or pooled-connection state may carry a previous principal's gates.
// Structural interfaces keep idempotency independent of tenant/project packages.
type authorityTransaction struct {
 pgx.Tx
 who auth.Identity
 tenant uuid.UUID
 required domain.Role
 gates []func(context.Context,domain.Role) error
}
func (tx *authorityTransaction) RegisterReceiptAuthority(gate func(context.Context,domain.Role)error) {
 if gate!=nil { tx.gates=append(tx.gates,gate) }
}
func (tx *authorityTransaction) ValidateReceiptAuthority(ctx context.Context) error {
 if err:=ctx.Err();err!=nil{return err}
 role,err:=AuthorizeInTransaction(ctx,tx.Tx,tx.who,tx.tenant,tx.required)
 if err!=nil{return err}
 for _,gate:=range tx.gates {
  if err=ctx.Err();err!=nil{return err}
  if err=gate(ctx,role);err!=nil{return err}
 }
 return ctx.Err()
}
