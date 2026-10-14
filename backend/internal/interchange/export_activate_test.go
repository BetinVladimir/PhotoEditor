package interchange
import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
type exportRuntimeRow struct{ready *bool}
func(r exportRuntimeRow)Scan(dest ...any)error{*dest[0].(**bool)=r.ready;return nil}
type exportActivationTx struct{runtime *bool;calls int}
func(t *exportActivationTx)QueryRow(_ context.Context,query string,_ ...any)pgx.Row{
 t.calls++
 if strings.Contains(query,"v30_export_pending_runtime_ready"){return exportRuntimeRow{t.runtime}}
 return activationRow{raw:[]byte("null")}
}
// Synthetic private receipt exercises only the activation primitive, not native
// qualification or a production end-to-end preparation path.
func exportActivationFixture(t *testing.T)(uuid.UUID,auth.Identity,CreateIntent,PreparedExportActivation){
 tid,who,intent,frozen,resolver:=exportExecutionFixture(t)
 _,compiled,err:=DecodeFrozenExportDocuments(context.Background(),intent,frozen.Body());if err!=nil{t.Fatal(err)}
 inventory,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);if err!=nil{t.Fatal(err)}
 task,err:=CompileExportTask(context.Background(),intent,frozen,inventory,ExportTaskResources{RAMBytes:2<<30,ScratchBytes:2<<30,OutputBytes:1<<30,MaxInputJSONBytes:128<<20,ContractsSHA:rawSHA([]byte("schemas"))});if err!=nil{t.Fatal(err)}
 return tid,who,intent,PreparedExportActivation{intent:intent,frozen:frozen,inventory:inventory,task:task}
}
func TestExportActivationTicketAndAuthorizedReplay(t *testing.T){
 tid,who,intent,p:=exportActivationFixture(t)
 for _,replay:=range []bool{false,true}{
  raw,_:=json.Marshal(map[string]any{"ticket":v2input.JobTicket{RunID:intent.Identity.Run,WorkflowID:intent.Identity.Workflow,State:"queued"},"replayed":replay})
  tx:=&activationTx{row:activationRow{raw:raw}}
  response,err:=p.Activate(context.Background(),tx,who,tid,intent.Request,"pipeline-key")
  if err!=nil||response.Status!=202||response.Replayed!=replay||tx.calls!=1||tx.args[4]!=intent.RequestSHA{t.Fatal("activation ticket",err)}
  var task v2input.DraftTask;if err:=json.Unmarshal([]byte(tx.args[5].(string)),&task);err!=nil||task.ID!=p.task.ID||task.InputDigest!=p.inventory.digest{t.Fatal("wrong task binding",err)}
 }
}
func TestExportActivationRejectsScopeTamperAndDatabaseFailures(t *testing.T){
 tid,who,intent,p:=exportActivationFixture(t);tx:=&activationTx{}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=p.Activate(ctx,tx,who,tid,intent.Request,"pipeline-key");err!=context.Canceled||tx.calls!=0{t.Fatal("cancelled SQL")}
 if _,err:=p.Activate(context.Background(),tx,who,uuid.New(),intent.Request,"pipeline-key");err==nil||tx.calls!=0{t.Fatal("foreign tenant SQL")}
 bad:=p;bad.task.InputDigest=rawSHA([]byte("substitution"))
 if _,err:=bad.Activate(context.Background(),tx,who,tid,intent.Request,"pipeline-key");err==nil||tx.calls!=0{t.Fatal("tampered task SQL")}
 if _,err:=(PreparedExportActivation{}).Activate(context.Background(),tx,who,tid,intent.Request,"pipeline-key");err==nil||tx.calls!=0{t.Fatal("empty receipt SQL")}
 cause:=errors.New("database unavailable");tx.row.err=cause
 if _,err:=p.Activate(context.Background(),tx,who,tid,intent.Request,"pipeline-key");!errors.Is(err,cause){t.Fatal("database cause lost",err)}
 tx.row.err=&pgconn.PgError{Code:"23514",Message:"INTERCHANGE_EXPORT_ACTIVATION_REFUSED"}
 if _,err:=p.Activate(context.Background(),tx,who,tid,intent.Request,"pipeline-key");err==nil{t.Fatal("seal refusal accepted")}
 stale:=&exportActivationTx{}
 if _,err:=p.Activate(context.Background(),stale,who,tid,intent.Request,"pipeline-key");err==nil||stale.calls!=2{t.Fatal("missing ticket accepted",err,stale.calls)}
 unavailable:=false
 withoutRuntime:=&exportActivationTx{runtime:&unavailable}
 if _,err:=p.Activate(context.Background(),withoutRuntime,who,tid,intent.Request,"pipeline-key");domain.AsError(err)==nil||domain.AsError(err).Code!="INTERCHANGE_UNAVAILABLE"||withoutRuntime.calls!=2{t.Fatal("missing export runtime not classified",err,withoutRuntime.calls)}
 if _,err:=(Service{}).ActivateExport(context.Background(),who,tid,intent.Request,"pipeline-key",p);err!=v2input.ErrInvalid{t.Fatal("missing database accepted")}
}
