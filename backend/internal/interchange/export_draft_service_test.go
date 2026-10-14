package interchange

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5"
)

type exportDraftTx struct{
 exportLedgerTx
 accepted bool
 failure error
 calls int
 args []any
}
func(tx *exportDraftTx)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{
 if strings.Contains(query,"begin_v30_interchange_export_run"){
  tx.calls++;tx.args=append([]any(nil),args...)
  return ledgerRow(func(dest ...any)error{if tx.failure!=nil{return tx.failure};*dest[0].(*bool)=tx.accepted;return nil})
 }
 return tx.exportLedgerTx.QueryRow(ctx,query,args...)
}
func TestPrepareExportDraftUsesPinnedEvidenceAndScopedSQL(t *testing.T){
 tid,who,intent,compiled,ref,raw:=exportLedgerFixture(t)
 proposed,err:=FreezeExportDocuments(context.Background(),intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 tx:=&exportDraftTx{exportLedgerTx:exportLedgerTx{ledgerTx:ledgerTx{allowed:true,live:true}},accepted:true}
 first,plan,err:=PrepareExportDraft(context.Background(),tx,who,tid,intent.Request,"export-key",proposed)
 if err!=nil||first.Identity()!=intent.Identity||plan.TaskID()!=compiled.TaskID(){t.Fatal("draft preparation failed",err)}
 if tx.calls!=1||len(tx.args)!=6||tx.args[0]!=tid||tx.args[1]!=who.UserID||tx.args[2]!=who.SessionID||tx.args[3]!="export-key"||tx.args[4]!=intent.RequestSHA{t.Fatal("draft SQL scope mismatch")}
 config,err:=exportDraftConfig(context.Background(),intent,first);if err!=nil||tx.args[5]!=string(config){t.Fatal("config differs from pinned evidence",err)}
 writes:=tx.writes
 replay,_,err:=PrepareExportDraft(context.Background(),tx,who,tid,intent.Request,"export-key",proposed)
 if err!=nil||string(replay.Body())!=string(first.Body())||tx.writes!=writes||tx.calls!=2{t.Fatal("replay replaced evidence",err)}
}
func TestPrepareExportDraftDoesNotExposeFailedDraft(t *testing.T){
 tid,who,intent,compiled,ref,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"denied","stale","database"}{t.Run(mode,func(t *testing.T){
  tx:=&exportDraftTx{exportLedgerTx:exportLedgerTx{ledgerTx:ledgerTx{allowed:mode!="denied",live:true}},accepted:mode!="stale"}
  if mode=="database"{tx.failure=errors.New("database failure")}
  got,plan,err:=PrepareExportDraft(context.Background(),tx,who,tid,intent.Request,"export-key",frozen)
  if err==nil||len(got.Body())!=0||plan.TaskID()!= (CompiledExportPlan{}).TaskID(){t.Fatal("failed draft exposed")}
  if mode=="denied"&&tx.calls!=0{t.Fatal("unauthorized draft SQL invoked")}
 })}
 if got,_,err:=(Service{}).PrepareExportDraft(context.Background(),who,tid,intent.Request,"export-key",frozen);err==nil||len(got.Body())!=0{t.Fatal("missing database accepted")}
}
