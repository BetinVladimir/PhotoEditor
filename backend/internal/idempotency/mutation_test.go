package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/canonical"
)

type receiptRow struct { scan func(...any) error }
func (r receiptRow) Scan(dst ...any) error { return r.scan(dst...) }
type receiptTx struct {
	pgx.Tx
	hash string
	status *int
	body []byte
	cancel context.CancelFunc
	calls int
	updated int64
}
func (tx *receiptTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.calls++
	if tx.calls == 1 { return pgconn.NewCommandTag("INSERT 0 1"), nil }
	if tx.updated == 1 { return pgconn.NewCommandTag("UPDATE 1"), nil }
	return pgconn.NewCommandTag("UPDATE 0"), nil
}
func (tx *receiptTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return receiptRow{scan: func(dst ...any) error {
		*(dst[0].(*uuid.UUID)) = uuid.New()
		*(dst[1].(*string)) = tx.hash
		*(dst[2].(**int)) = tx.status
		*(dst[3].(*[]byte)) = tx.body
		*(dst[4].(**uuid.UUID)) = nil
		if tx.cancel != nil { tx.cancel() }
		return nil
	}}
}
func testScope() Scope { return Scope{Tenant: uuid.New(), Principal: uuid.New(), PrincipalType: "user", Route: "test", Key: "key"} }

func TestCanceledBeforeReceiptDoesNotTouchDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background()); cancel()
	_, err := Use(ctx, nil, testScope(), map[string]any{}, time.Now(), func() (Response,error) { t.Fatal("mutation invoked"); return Response{},nil })
	if !errors.Is(err, context.Canceled) { t.Fatalf("error = %v", err) }
}
func TestCancellationWhileWaitingForReceiptRefusesMutationAndReplay(t *testing.T) {
	for _, replay := range []bool{false,true} {
		t.Run(map[bool]string{false:"mutation",true:"replay"}[replay], func(t *testing.T) {
			ctx,cancel := context.WithCancel(context.Background()); defer cancel()
			hash,_ := canonical.Hash(map[string]any{})
			tx := &receiptTx{hash:hash,cancel:cancel,body:[]byte(`{"ok":true}`)}
			if replay { status:=200; tx.status=&status }
			_,err := Use(ctx,tx,testScope(),map[string]any{},time.Now(),func()(Response,error){ t.Fatal("mutation invoked");return Response{},nil })
			if !errors.Is(err,context.Canceled) || tx.calls != 1 { t.Fatalf("err=%v calls=%d",err,tx.calls) }
		})
	}
}
func TestMissingReceiptUpdateCannotReturnSuccess(t *testing.T) {
	hash,_ := canonical.Hash(map[string]any{})
	tx:=&receiptTx{hash:hash}
	_,err:=Use(context.Background(),tx,testScope(),map[string]any{},time.Now(),func()(Response,error){return Response{Status:201,Body:json.RawMessage(`{"ok":true}`)},nil})
	if err == nil || tx.calls != 2 { t.Fatalf("err=%v calls=%d",err,tx.calls) }
}
func TestCancellationAfterMutationRefusesReceiptPublication(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	hash,_:=canonical.Hash(map[string]any{});tx:=&receiptTx{hash:hash,updated:1}
	_,err:=Use(ctx,tx,testScope(),map[string]any{},time.Now(),func()(Response,error){cancel();return Response{Status:201,Body:json.RawMessage(`{}`)},nil})
	if !errors.Is(err,context.Canceled) || tx.calls != 1 { t.Fatalf("err=%v calls=%d",err,tx.calls) }
}

func TestSuccessfulMutationAndCanonicalReplay(t *testing.T) {
	hash,_:=canonical.Hash(map[string]any{})
	tx:=&receiptTx{hash:hash,updated:1}
	out,err:=Use(context.Background(),tx,testScope(),map[string]any{},time.Now(),func()(Response,error){return Response{Status:201,Body:json.RawMessage(`{"z":2,"a":1}`)},nil})
	if err != nil || string(out.Body) != `{"a":1,"z":2}` || out.Replayed { t.Fatalf("out=%+v err=%v",out,err) }
	status:=201;tx=&receiptTx{hash:hash,status:&status,body:out.Body}
	out,err=Use(context.Background(),tx,testScope(),map[string]any{},time.Now(),func()(Response,error){t.Fatal("replayed mutation invoked");return Response{},nil})
	if err != nil || !out.Replayed || out.Status != 201 || string(out.Body) != `{"a":1,"z":2}` || tx.calls != 1 { t.Fatalf("out=%+v err=%v",out,err) }
}
func TestChangedRequestCannotInvokeMutation(t *testing.T) {
	hash,_:=canonical.Hash(map[string]int{"revision":1})
	tx:=&receiptTx{hash:hash}
	_,err:=Use(context.Background(),tx,testScope(),map[string]int{"revision":2},time.Now(),func()(Response,error){t.Fatal("conflicting mutation invoked");return Response{},nil})
	if err == nil || tx.calls != 1 { t.Fatalf("err=%v calls=%d",err,tx.calls) }
}

func TestMalformedReceiptCannotBecomeFinalRefusalOrSuccess(t *testing.T) {
 for _, item := range []struct { name string; status int; body string }{
  {"temporary_refusal",503,`{"error":"busy"}`},
  {"domain_refusal",409,`{"error":"conflict"}`},
  {"malformed_success",200,`{"incomplete":`},
 } {
  t.Run(item.name,func(t *testing.T) {
   hash,_:=canonical.Hash(map[string]any{})
   tx:=&receiptTx{hash:hash,status:&item.status,body:[]byte(item.body)}
   out,err:=Use(context.Background(),tx,testScope(),map[string]any{},time.Now(),func()(Response,error){t.Fatal("malformed receipt invoked mutation");return Response{},nil})
   if err==nil || out.Replayed || out.Status!=0 || len(out.Body)!=0 || tx.calls!=1 { t.Fatalf("out=%+v err=%v calls=%d",out,err,tx.calls) }
  })
 }
}

type guardedReceiptTx struct { *receiptTx; authorityErr error; validations int }
func(tx *guardedReceiptTx)ValidateReceiptAuthority(context.Context)error{tx.validations++;return tx.authorityErr}
func TestPostWaitAuthorityRefusalBlocksMutationAndReplay(t *testing.T){
 for _,replay:=range []bool{false,true}{
  hash,_:=canonical.Hash(map[string]any{})
  base:=&receiptTx{hash:hash,body:[]byte(`{"ok":true}`)}
  if replay{status:=200;base.status=&status}
  denied:=errors.New("project authority revoked while waiting")
  tx:=&guardedReceiptTx{receiptTx:base,authorityErr:denied}
  out,err:=Use(context.Background(),tx,testScope(),map[string]any{},time.Now(),func()(Response,error){t.Fatal("mutation ran after authority refusal");return Response{},nil})
  if !errors.Is(err,denied) || out.Replayed || len(out.Body)!=0 || tx.validations!=1 || base.calls!=1{t.Fatalf("out=%+v err=%v validations=%d calls=%d",out,err,tx.validations,base.calls)}
 }
}
