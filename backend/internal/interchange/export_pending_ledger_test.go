package interchange
import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type exportLedgerTx struct{ledgerTx}
func(tx *exportLedgerTx)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{
 if strings.Contains(query,"v25_lens_snapshot_ready"){return ledgerRow(func(d ...any)error{*d[0].(*bool)=tx.allowed;return nil})}
 return tx.ledgerTx.QueryRow(ctx,query,args...)
}
func exportLedgerFixture(t *testing.T)(uuid.UUID,auth.Identity,CreateIntent,CompiledExportPlan,v2input.Ref,[]byte){
 t.Helper();_,project,snapshotRef,raw,frame:=exportPlanFixture(t)
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument(raw,&snapshot);err!=nil{t.Fatal(err)}
 tenant:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};id:=snapshot.SnapshotID
 intent,err:=FreezeCreateIntent(tenant,who,CreateRequest{ProjectID:project,Direction:"export",Format:"xmp",SnapshotID:&id},"export-key");if err!=nil{t.Fatal(err)}
 compiled,err:=CompileExportPlan(context.Background(),intent.Identity.Workflow,project,"xmp",snapshotRef,raw,ExportLimits{1<<20,1<<20,10},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return frame,nil}));if err!=nil{t.Fatal(err)}
 return tenant,who,intent,compiled,snapshotRef,raw
}
func TestExportPendingLedgerPinsAndRecoversOriginalDocuments(t *testing.T){
 tenant,who,intent,compiled,snapshot,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 tx:=&exportLedgerTx{ledgerTx:ledgerTx{allowed:true,live:true}}
 first,_,err:=PinExportPending(context.Background(),tx,who,tenant,intent.Request,"export-key",frozen);if err!=nil{t.Fatal(err)}
 second,plan,err:=PinExportPending(context.Background(),tx,who,tenant,intent.Request,"export-key",frozen)
 if err!=nil||string(first.Body())!=string(second.Body())||tx.writes!=1||plan.TaskID()!=compiled.TaskID(){t.Fatal("unstable pending replay",err)}
}
func TestExportPendingLedgerRefusesAccessConflictExpiryAndCompletedRecord(t *testing.T){
 tenant,who,intent,compiled,snapshot,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 status:=202
 for _,state:=range []ledgerTx{{allowed:false,live:true},{allowed:true,live:true,hash:"wrong"},{allowed:true,live:false},{allowed:true,live:true,status:&status}}{
  tx:=&exportLedgerTx{ledgerTx:state}
  if _,_,err:=PinExportPending(context.Background(),tx,who,tenant,intent.Request,"export-key",frozen);err==nil||tx.writes!=0{t.Fatal("invalid ledger accepted",err)}
 }
}
