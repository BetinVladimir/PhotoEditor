package idempotency

import "context"

// ValidateReceiptAuthority is also used by private pending ledgers that lock
// receipts directly. The transaction owns any typed validation closures.
func ValidateReceiptAuthority(ctx context.Context, tx any) error {
 if err:=ctx.Err();err!=nil{return err}
 if guarded,ok:=tx.(interface{ValidateReceiptAuthority(context.Context)error});ok{
  if err:=guarded.ValidateReceiptAuthority(ctx);err!=nil{return err}
 }
 return ctx.Err()
}
