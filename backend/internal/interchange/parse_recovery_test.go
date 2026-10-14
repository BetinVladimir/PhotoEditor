package interchange
import("context";"encoding/json";"strings";"testing";"github.com/google/uuid";"github.com/jackc/pgx/v5";"photoeditor/backend/internal/v2input")
type completedParseRecoveryTx struct{calls int;hash string;body []byte;workflow uuid.UUID;committed bool}
type completedParseRecoveryRow struct{scan func(...any)error}
func(r completedParseRecoveryRow)Scan(dest ...any)error{return r.scan(dest...)}
func(tx *completedParseRecoveryTx)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 tx.calls++
 switch tx.calls{
 case 1:return completedParseRecoveryRow{func(dest ...any)error{*dest[0].(*bool)=true;return nil}}
 case 2:return completedParseRecoveryRow{func(dest ...any)error{
  *dest[0].(*string)=tx.hash;*dest[1].(*[]byte)=tx.body;*dest[2].(*bool)=true
  status:=202;*dest[3].(**int)=&status;*dest[4].(**uuid.UUID)=&tx.workflow;return nil
 }}
 case 3:
  if !strings.Contains(query,"photo.backend_documents")||!strings.Contains(query,"v30_interchange_proof")||len(args)!=11{panic("committed replay omitted params proof")}
  return completedParseRecoveryRow{func(dest ...any)error{*dest[0].(*bool)=tx.committed;return nil}}
 default:panic("completed ticket read attempted pending or source lookup")
 }
}
func TestRecoverParseRejectsAccessLossBeforeLedgerRead(t *testing.T){
 tenant,who,request,_,_:=pendingFixture();tx:=&proofTx{accepted:false}
 if _,err:=RecoverParse(context.Background(),tx,who,tenant,request,"key");err==nil{t.Fatal("access loss accepted")}
 if len(tx.args)!=4||tx.args[0]!=tenant||tx.args[1]!=request.ProjectID||tx.args[2]!=who.UserID||tx.args[3]!=who.SessionID{t.Fatal("recovery did not start with current project ACL")}
 tx.args=nil;ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=RecoverParse(ctx,tx,who,tenant,request,"key");err!=context.Canceled||tx.args!=nil{t.Fatal("cancelled recovery queried SQL")}
}
func TestRecoverParseCompletedTicketDoesNotRequireExpiredSource(t *testing.T){
 tenant,who,request,_,_:=pendingFixture()
 intent,err:=FreezeCreateIntent(tenant,who,request,"key");if err!=nil{t.Fatal(err)}
 ticket:=v2input.JobTicket{RunID:intent.Identity.Run,WorkflowID:intent.Identity.Workflow,State:"queued"}
 body,err:=json.Marshal(ticket);if err!=nil{t.Fatal(err)}
 tx:=&completedParseRecoveryTx{hash:intent.RequestSHA,body:body,workflow:intent.Identity.Workflow,committed:true}
 result,err:=RecoverParse(context.Background(),tx,who,tenant,request,"key")
 if err!=nil||!result.Completed||result.Response.Status!=202||!result.Response.Replayed||tx.calls!=3{t.Fatal("completed ticket was not replayed directly",err,result,tx.calls)}
 tx=&completedParseRecoveryTx{hash:"different-request",body:body,workflow:intent.Identity.Workflow}
 if _,err:=RecoverParse(context.Background(),tx,who,tenant,request,"key");err==nil||tx.calls!=2{t.Fatal("conflicting ticket accepted or source queried",err,tx.calls)}
 tx=&completedParseRecoveryTx{hash:intent.RequestSHA,body:body,workflow:uuid.New()}
 if _,err:=RecoverParse(context.Background(),tx,who,tenant,request,"key");err==nil||tx.calls!=2{t.Fatal("foreign workflow ticket accepted or source queried",err,tx.calls)}
 tx=&completedParseRecoveryTx{hash:intent.RequestSHA,body:body,workflow:intent.Identity.Workflow}
 if _,err:=RecoverParse(context.Background(),tx,who,tenant,request,"key");err==nil||tx.calls!=3{t.Fatal("orphan ticket accepted",err,tx.calls)}
}
