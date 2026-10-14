package interchange

import (
 "context"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
)
type ledgerRow func(...any)error
func(r ledgerRow)Scan(dest ...any)error{return r(dest...)}
type ledgerTx struct {allowed,live bool;hash string;body []byte;size int64;writes int;status *int;sourceArgs []any}
func(tx *ledgerTx)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 switch {
 case strings.Contains(query,"v2_editor_authorized"):
  return ledgerRow(func(d ...any)error{*d[0].(*bool)=tx.allowed;return nil})
 case strings.Contains(query,"FROM photo.idempotency_records"):
  return ledgerRow(func(d ...any)error{*d[0].(*string)=tx.hash;*d[1].(*[]byte)=tx.body;*d[2].(*bool)=tx.live;*d[3].(**int)=tx.status;return nil})
 case strings.Contains(query,"SELECT a.bytes"):
  tx.sourceArgs=args
  return ledgerRow(func(d ...any)error{*d[0].(*int64)=tx.size;return nil})
 default:panic("unexpected query")
 }
}
func(tx *ledgerTx)Exec(_ context.Context,query string,args ...any)(pgconn.CommandTag,error){
 if strings.Contains(query,"INSERT INTO") {if tx.hash==""{tx.hash=args[4].(string)};return pgconn.NewCommandTag("INSERT 0 1"),nil}
 tx.body=[]byte(args[4].(string));tx.writes++;return pgconn.NewCommandTag("UPDATE 1"),nil
}
func TestPendingLedgerReplaysOriginalBudgetAndSourceScope(t *testing.T){
 tid,who,request,_,limits:=pendingFixture();tx:=&ledgerTx{allowed:true,live:true,size:100}
 first,err:=PinParsePending(context.Background(),tx,who,tid,request,"key",limits);if err!=nil{t.Fatal(err)}
 if tx.writes!=1||len(tx.sourceArgs)!=4||tx.sourceArgs[0]!=tid||tx.sourceArgs[1]!=request.ProjectID||tx.sourceArgs[2]!=request.Bundle.ArtifactID{t.Fatal("source scope not pinned")}
 second,err:=PinParsePending(context.Background(),tx,who,tid,request,"key",ParseLimits{});if err!=nil||first.Identity()!=second.Identity()||first.OutputBytes()!=second.OutputBytes()||tx.writes!=1{t.Fatal("replay changed budget",err)}
 tx.size=101;if _,err:=PinParsePending(context.Background(),tx,who,tid,request,"key",limits);err==nil{t.Fatal("changed source size accepted")}
}
func TestPendingLedgerRefusesAccessConflictAndExpiryBeforeSourceRead(t *testing.T){
 tid,who,request,_,limits:=pendingFixture();intent,_:=FreezeCreateIntent(tid,who,request,"key")
 for _,tx:=range []*ledgerTx{
  {allowed:false,live:true,size:100},
  {allowed:true,live:true,size:100,hash:"wrong"},
  {allowed:true,live:false,size:100,hash:intent.RequestSHA},
 }{
  if _,err:=PinParsePending(context.Background(),tx,who,tid,request,"key",limits);err==nil||tx.writes!=0||tx.sourceArgs!=nil{t.Fatal("unauthorized/conflicting/expired source read",err)}
 }
}
