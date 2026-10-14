package interchange
import("context";"encoding/json";"errors";"testing";"github.com/google/uuid";"github.com/jackc/pgx/v5";"github.com/jackc/pgx/v5/pgconn";"photoeditor/backend/internal/v2input")
type activationRow struct{raw []byte;err error}
func(r activationRow)Scan(dest ...any)error{if r.err!=nil{return r.err};*dest[0].(*[]byte)=append([]byte(nil),r.raw...);return nil}
type activationTx struct{row activationRow;calls int;args []any}
func(t *activationTx)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{t.calls++;t.args=args;return t.row}
func TestParseTicketStrictIdentityAndShape(t *testing.T){
 identity:=CreateIdentity{Run:uuid.New(),Workflow:uuid.New()}
 good:=map[string]any{"ticket":v2input.JobTicket{RunID:identity.Run,WorkflowID:identity.Workflow,State:"queued"},"replayed":false}
 raw,_:=json.Marshal(good);result,err:=decodeParseTicket(raw,identity)
 if err!=nil||result.Status!=202||result.Replayed||result.ResourceID==nil||*result.ResourceID!=identity.Workflow{t.Fatal("valid ticket refused",err)}
 for _,bad:=range [][]byte{nil,[]byte("null"),[]byte("{}"),append(raw,[]byte("{}")...)}{if _,err:=decodeParseTicket(bad,identity);err==nil{t.Fatal("invalid ticket accepted")}}
 other:=identity;other.Run=uuid.New();if _,err:=decodeParseTicket(raw,other);err==nil{t.Fatal("foreign ticket accepted")}
 good["extra"]=true;raw,_=json.Marshal(good);if _,err:=decodeParseTicket(raw,identity);err==nil{t.Fatal("unknown field accepted")}
}
func TestActivateParsePreservesDatabaseErrorsAndPreflight(t *testing.T){
 tenant,who,request,evidence,limits:=pendingFixture();plan,err:=CompileParsePlan(tenant,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 original:=errors.New("database unavailable");tx:=&activationTx{row:activationRow{err:original}}
 if _,err:=plan.ActivateParse(context.Background(),tx,who,tenant,request,"key");!errors.Is(err,original){t.Fatal("database cause lost",err)}
 tx.row.err=&pgconn.PgError{Code:"23514",Message:"INTERCHANGE_INITIAL_ACTIVATION_REFUSED"}
 if _,err:=plan.ActivateParse(context.Background(),tx,who,tenant,request,"key");err==nil{t.Fatal("seal refusal ignored")}
 tx.calls=0;ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=plan.ActivateParse(ctx,tx,who,tenant,request,"key");err!=context.Canceled||tx.calls!=0{t.Fatal("cancelled SQL")}
 if _,err:=plan.ActivateParse(context.Background(),tx,who,uuid.New(),request,"key");err==nil||tx.calls!=0{t.Fatal("foreign tenant queried")}
}
