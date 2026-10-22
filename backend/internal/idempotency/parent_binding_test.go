package idempotency

import (
 "context"
 "errors"
 "testing"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
)

type bindingTx struct { pgx.Tx; stored *string; updated int64; writes int }
func (tx *bindingTx) QueryRow(context.Context,string,...any) pgx.Row {
 return receiptRow{scan:func(dst ...any) error { *(dst[0].(**string))=tx.stored;return nil }}
}
func (tx *bindingTx) Exec(context.Context,string,...any)(pgconn.CommandTag,error){
 tx.writes++
 if tx.updated==1{return pgconn.NewCommandTag("UPDATE 1"),nil}
 return pgconn.NewCommandTag("UPDATE 0"),nil
}
func TestParentBindingPreservesExactReplayAndRefusesOtherParent(t *testing.T){
 parent:="collection:one"
 for _, requested:=range []string{parent,"collection:two"}{
  tx:=&bindingTx{stored:&parent}
  err:=BindParent(context.Background(),tx,testScope(),Response{Status:201,Replayed:true},requested,func()error{t.Fatal("existing binding invoked legacy proof");return nil})
  if (err==nil)!=(requested==parent) || tx.writes!=0 {t.Fatalf("parent=%s err=%v writes=%d",requested,err,tx.writes)}
 }
}
func TestParentBindingRequiresProofOnlyForLegacyReplay(t *testing.T){
 for _, replay:=range []bool{false,true}{
  tx:=&bindingTx{updated:1};proofs:=0
  err:=BindParent(context.Background(),tx,testScope(),Response{Status:201,Replayed:replay},"style:one",func()error{proofs++;return nil})
  if err!=nil || tx.writes!=1 || (proofs==1)!=replay {t.Fatalf("replay=%v err=%v proofs=%d writes=%d",replay,err,proofs,tx.writes)}
 }
}
func TestParentBindingUnknownLegacyAndFailedPersistenceRefuse(t *testing.T){
 sentinel:=errors.New("unprovable legacy parent")
 for _, proof:=range []func()error{nil,func()error{return sentinel}}{
  tx:=&bindingTx{updated:1}
  if err:=BindParent(context.Background(),tx,testScope(),Response{Status:201,Replayed:true},"feedback:one",proof);err==nil || tx.writes!=0{t.Fatalf("err=%v writes=%d",err,tx.writes)}
 }
 tx:=&bindingTx{}
 if err:=BindParent(context.Background(),tx,testScope(),Response{Status:201},"style:one",nil);err==nil{t.Fatal("missing binding update accepted")}
}
