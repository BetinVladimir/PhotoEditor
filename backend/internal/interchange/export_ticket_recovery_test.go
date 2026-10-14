package interchange
import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type exportTicketRecoveryTx struct{calls int;allowed,live,committed bool;hash string;body []byte;status *int;resource *uuid.UUID;err error;absent bool;queries []string}
func(t *exportTicketRecoveryTx)QueryRow(_ context.Context,query string,_ ...any)pgx.Row{
 t.calls++;t.queries=append(t.queries,query)
 return ledgerRow(func(dest ...any)error{
  if t.err!=nil{return t.err}
  switch{
  case strings.Contains(query,"v2_editor_authorized"):*dest[0].(*bool)=t.allowed
  case strings.Contains(query,"Interchange.Export.Ticket"):
   if t.absent{return pgx.ErrNoRows};*dest[0].(*string)=t.hash;*dest[1].(*[]byte)=append([]byte(nil),t.body...);*dest[2].(*bool)=t.live;*dest[3].(**int)=t.status;*dest[4].(**uuid.UUID)=t.resource
  default:*dest[0].(*bool)=t.committed
  };return nil
 })
}
func TestExportTicketRecoveryCurrentAuthorityAndReceiptBoundaries(t *testing.T){
 for _,mode:=range []string{"valid","absent","denied","conflict","expired","status","resource","malformed","foreign_ticket","uncommitted","database","cancelled"}{t.Run(mode,func(t *testing.T){
  tid,who,intent,_:=exportActivationFixture(t);status:=202;resource:=intent.Identity.Workflow
  body,_:=json.Marshal(v2input.JobTicket{RunID:intent.Identity.Run,WorkflowID:intent.Identity.Workflow,State:"queued"})
  tx:=&exportTicketRecoveryTx{allowed:true,live:true,committed:true,hash:intent.RequestSHA,body:body,status:&status,resource:&resource};cause:=errors.New("database unavailable")
  ctx:=context.Background()
  switch mode{case "absent":tx.absent=true;case "denied":tx.allowed=false;case "conflict":tx.hash=rawSHA([]byte("foreign"));case "expired":tx.live=false;case "status":tx.status=nil;case "resource":tx.resource=nil;case "malformed":tx.body=[]byte("{}");case "foreign_ticket":tx.body,_=json.Marshal(v2input.JobTicket{RunID:uuid.New(),WorkflowID:resource,State:"queued"});case "uncommitted":tx.committed=false;case "database":tx.err=cause;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  response,found,err:=RecoverExportTicket(ctx,tx,who,tid,intent.Request,"pipeline-key")
  switch mode{
  case "valid":if err!=nil||!found||response.Status!=202||!response.Replayed||response.ResourceID==nil||*response.ResourceID!=resource{t.Fatal("valid recovery",err)}
  case "absent":if err!=nil||found||response.Status!=0{t.Fatal("missing ticket invented",err)}
  default:if err==nil||found||response.Status!=0{t.Fatal("invalid recovery exposed ticket",err)}
  }
  if mode=="denied"&&tx.calls!=1{t.Fatal("denied read ledger")}
  if mode=="cancelled"&&tx.calls!=0{t.Fatal("cancelled SQL")}
  if mode=="database"&&!errors.Is(err,cause){t.Fatal("database error lost")}
  for _,query:=range tx.queries{if strings.Contains(query,"Interchange.Export.Pending"){t.Fatal("ticket depends on pending ledger")}}
 })}
}
func TestExportTicketRecoveryRequiresDatabaseAndExport(t *testing.T){
 tid,who,intent,_:=exportActivationFixture(t)
 if _,found,err:=RecoverExportTicket(context.Background(),nil,who,tid,intent.Request,"pipeline-key");err==nil||found{t.Fatal("missing transaction accepted")}
 if _,found,err:=(Service{}).RecoverExportTicket(context.Background(),who,tid,intent.Request,"pipeline-key");err==nil||found{t.Fatal("missing database accepted")}
}
