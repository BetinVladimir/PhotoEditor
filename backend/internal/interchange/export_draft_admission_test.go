package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type exportDraftAdmissionTx struct{calls int;args []any;ready bool;err error}
func(t *exportDraftAdmissionTx)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{
 t.calls++;t.args=args
 return ledgerRow(func(dest ...any)error{if t.err!=nil{return t.err};*dest[0].(*bool)=t.ready;return nil})
}
func TestExportDraftAdmissionCurrentAuthorityAndRefusal(t *testing.T){
 tid,who,intent,frozen,resolver:=exportExecutionFixture(t)
 _,compiled,err:=DecodeFrozenExportDocuments(context.Background(),intent,frozen.Body());if err!=nil{t.Fatal(err)}
 inventory,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);if err!=nil{t.Fatal(err)}
 tx:=&exportDraftAdmissionTx{ready:true}
 if err:=CheckExportDraftAdmission(context.Background(),tx,who,tid,intent.Request,"pipeline-key",frozen,inventory);err!=nil{t.Fatal(err)}
 if tx.calls!=1||tx.args[0]!=tid||tx.args[4]!=intent.RequestSHA||tx.args[5]!=inventory.digest{t.Fatal("scope/digest mismatch")}
 tx.ready=false;if err:=CheckExportDraftAdmission(context.Background(),tx,who,tid,intent.Request,"pipeline-key",frozen,inventory);err==nil{t.Fatal("stale authority accepted")}
 original:=errors.New("database unavailable");tx.err=original
 if err:=CheckExportDraftAdmission(context.Background(),tx,who,tid,intent.Request,"pipeline-key",frozen,inventory);!errors.Is(err,original){t.Fatal("database cause lost",err)}
 tx.calls=0;ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=CheckExportDraftAdmission(ctx,tx,who,tid,intent.Request,"pipeline-key",frozen,inventory);err!=context.Canceled||tx.calls!=0{t.Fatal("cancelled SQL")}
 if err:=CheckExportDraftAdmission(context.Background(),tx,who,uuid.New(),intent.Request,"pipeline-key",frozen,inventory);err==nil||tx.calls!=0{t.Fatal("foreign tenant SQL")}
 if err:=CheckExportDraftAdmission(context.Background(),nil,who,tid,intent.Request,"pipeline-key",frozen,inventory);err!=v2input.ErrInvalid{t.Fatal("missing transaction")}
 bad:=inventory;bad.digest="invalid"
 if err:=CheckExportDraftAdmission(context.Background(),tx,who,tid,intent.Request,"pipeline-key",frozen,bad);err==nil||tx.calls!=0{t.Fatal("tampered inventory SQL")}
}
