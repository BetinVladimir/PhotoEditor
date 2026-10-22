package semanticreview

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type receiptAuthorityTx struct {
 pgx.Tx
 tenant,intent,actor,session uuid.UUID
 identity ReviewIdentity
}
func(tx receiptAuthorityTx)ValidateReceiptAuthority(ctx context.Context)error{
 if err:=ctx.Err();err!=nil{return err}
 current,err:=LoadReviewIdentity(ctx,tx.Tx,tx.tenant,tx.intent,tx.actor,tx.session)
 if err!=nil{return err}
 if current!=tx.identity{return ErrConflict}
 return ctx.Err()
}
func guardReceiptAuthority(tx pgx.Tx,tenant,intent,actor,session uuid.UUID,identity ReviewIdentity)pgx.Tx{
 return receiptAuthorityTx{Tx:tx,tenant:tenant,intent:intent,actor:actor,session:session,identity:identity}
}
