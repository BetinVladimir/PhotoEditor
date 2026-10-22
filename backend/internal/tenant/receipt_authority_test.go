package tenant

import (
 "context"
 "errors"
 "testing"

 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
)

type authorityRow struct { role domain.Role; err error }
func(r authorityRow) Scan(dst ...any)error{
 if r.err!=nil{return r.err}
 *(dst[0].(*domain.Role))=r.role
 *(dst[1].(*int64))=1
 return nil
}
type authoritySQL struct { pgx.Tx; role domain.Role; err error; reads int }
func(tx *authoritySQL) QueryRow(context.Context,string,...any)pgx.Row{
 tx.reads++;return authorityRow{role:tx.role,err:tx.err}
}
func TestReceiptAuthorityUsesLiveRoleAndTransactionLocalGates(t *testing.T){
 sql:=&authoritySQL{role:domain.Viewer}
 first:=&authorityTransaction{Tx:sql,required:domain.Viewer}
 second:=&authorityTransaction{Tx:sql,required:domain.Viewer}
 calls:=0
 first.RegisterReceiptAuthority(func(ctx context.Context,role domain.Role)error{
  calls++
  if role!=domain.Viewer{t.Fatal("preflight role reused")}
  return domain.NewError("FORBIDDEN",403)
 })
 if err:=first.ValidateReceiptAuthority(context.Background());err==nil{t.Fatal("revoked project gate accepted")}
 if err:=second.ValidateReceiptAuthority(context.Background());err!=nil{t.Fatal(err)}
 if calls!=1 || sql.reads!=2{t.Fatalf("calls=%d reads=%d",calls,sql.reads)}
}
func TestReceiptAuthorityCancellationAndMissingMembershipStopGates(t *testing.T){
 for _, canceled:=range []bool{false,true}{
  ctx,cancel:=context.WithCancel(context.Background())
  sql:=&authoritySQL{err:pgx.ErrNoRows}
  tx:=&authorityTransaction{Tx:sql,required:domain.Viewer}
  tx.RegisterReceiptAuthority(func(context.Context,domain.Role)error{t.Fatal("gate ran without live tenant authority");return nil})
  if canceled{cancel()}
  err:=tx.ValidateReceiptAuthority(ctx)
  cancel()
  if err==nil || (canceled && !errors.Is(err,context.Canceled)){t.Fatalf("canceled=%v err=%v",canceled,err)}
 }
}
