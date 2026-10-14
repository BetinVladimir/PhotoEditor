package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type exportRecoveryTx struct{raw []byte;calls int;args []any}
func(s *exportRecoveryTx)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{
 s.calls++;s.args=args;return ledgerRow(func(dest ...any)error{*(dest[0].(*[]byte))=append([]byte(nil),s.raw...);return nil})
}
func TestExportAdoptionRecoveryBindsIdentityAndTerminalStatus(t *testing.T){
 tid,who,intent,_,_,_:=exportLedgerFixture(t)
 ref:=func(value string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(value))}}
 baseline:=ExportAdoptionRecovery{Schema:1,Workflow:intent.Identity.Workflow,Run:intent.Identity.Run,Task:intent.Identity.Task,Result:uuid.New(),Status:"adopted",Root:ref("root"),Report:ref("report"),Bundle:ref("bundle"),EvidenceSHA:rawSHA([]byte("evidence"))}
 for _,kind:=range []string{"adopted","completed","wrong_run","wrong_task","status","alias","digest","null"}{t.Run(kind,func(t *testing.T){
  receipt:=baseline
  switch kind{case "completed":receipt.Status="completed";case "wrong_run":receipt.Run=uuid.New();case "wrong_task":receipt.Task=uuid.New();case "status":receipt.Status="running";case "alias":receipt.Bundle=receipt.Root;case "digest":receipt.EvidenceSHA="bad"}
  raw,err:=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)};if kind=="null"{raw=[]byte("null")}
  tx:=&exportRecoveryTx{raw:raw}
  recovered,found,err:=RecoverExportAdoption(context.Background(),tx,who,tid,intent.Request,"export-key")
  switch kind{case "adopted","completed":if err!=nil||!found||recovered.Status!=receipt.Status||len(tx.args)!=5||tx.args[4]!=intent.RequestSHA{t.Fatal("recovery",err)};case "null":if err!=nil||found{t.Fatal("absent recovery",err)};default:if err==nil||found{t.Fatal("invalid recovery accepted")}}
 })}
 ctx,cancel:=context.WithCancel(context.Background());cancel();tx:=&exportRecoveryTx{}
 if _,_,err:=RecoverExportAdoption(ctx,tx,who,tid,intent.Request,"export-key");err!=context.Canceled||tx.calls!=0{t.Fatal("cancelled recovery",err)}
 if _,_,err:=(Service{}).RecoverExportAdoption(context.Background(),who,tid,intent.Request,"export-key");err==nil{t.Fatal("missing DB accepted")}
}
