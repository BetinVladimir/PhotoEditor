package interchange
import (
 "context"
 "testing"
)
func TestExportRecoveryUsesSavedDocumentsWithoutResolverOrWrites(t *testing.T){
 tid,who,intent,compiled,snapshot,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 tx:=&exportLedgerTx{ledgerTx:ledgerTx{allowed:true,live:true}}
 if _,_,err:=PinExportPending(context.Background(),tx,who,tid,intent.Request,"export-key",frozen);err!=nil{t.Fatal(err)}
 before:=tx.writes
 recovery,err:=RecoverExportPending(context.Background(),tx,who,tid,intent.Request,"export-key")
 if err!=nil||!recovery.HasPending||recovery.Plan.TaskID()!=compiled.TaskID()||string(recovery.Frozen.Body())!=string(frozen.Body())||tx.writes!=before{t.Fatal("invalid recovery",err)}
}
func TestExportRecoveryRejectsInvalidLedgerAndRevokedAccess(t *testing.T){
 tid,who,intent,compiled,snapshot,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 tx:=&exportLedgerTx{ledgerTx:ledgerTx{allowed:true,live:true}}
 if _,_,err:=PinExportPending(context.Background(),tx,who,tid,intent.Request,"export-key",frozen);err!=nil{t.Fatal(err)}
 valid:=tx.ledgerTx
 for _,mode:=range []string{"denied","conflict","expired","body"}{t.Run(mode,func(t *testing.T){
  tx.ledgerTx=valid
  switch mode{case "denied":tx.allowed=false;case "conflict":tx.hash="wrong";case "expired":tx.live=false;case "body":tx.body=[]byte(`{}`)}
  result,err:=RecoverExportPending(context.Background(),tx,who,tid,intent.Request,"export-key")
  if err==nil||result.HasPending{t.Fatal("invalid recovery exposed",err)}
 })}
}
