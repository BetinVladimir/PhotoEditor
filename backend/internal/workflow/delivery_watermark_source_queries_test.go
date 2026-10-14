package workflow

import (
 "context"
 "errors"
 "reflect"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
)

type watermarkSourceQueryTx struct {watermarkParamsTx; failure error; commitFailure error}
type watermarkSourceQueryRow struct{failure error}
func(r watermarkSourceQueryRow)Scan(dest ...any)error{
 if r.failure!=nil{return r.failure};*dest[0].(*[]byte)=[]byte(`{"items":[],"next":null}`);return nil
}
func(t *watermarkSourceQueryTx)QueryRow(_ context.Context,q string,_ ...any)pgx.Row{
 if q!=watermarkChildSourcesQuery{panic("unexpected query")};t.events=append(t.events,"query");return watermarkSourceQueryRow{t.failure}
}
func(t *watermarkSourceQueryTx)Commit(context.Context)error{t.events=append(t.events,"commit");return t.commitFailure}
type watermarkSourceQueryPool struct{tx *watermarkSourceQueryTx;begins int}
func(p *watermarkSourceQueryPool)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){p.begins++;return p.tx,nil}

func TestWatermarkSourceQueriesCommitBeforeReturningInventory(t *testing.T){
 failure:=errors.New("database refused")
 for _,scenario:=range []string{"success","query","commit"}{t.Run(scenario,func(t *testing.T){
  tx:=&watermarkSourceQueryTx{};pool:=&watermarkSourceQueryPool{tx:tx}
  if scenario=="query"{tx.failure=failure};if scenario=="commit"{tx.commitFailure=failure}
  lease:=delivery.ExpansionLease{Tenant:uuid.New(),Run:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),ParentTask:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),Until:time.Now().Add(time.Minute),SlotCount:1}
  q:=watermarkChildSourceQueries{db:pool,lease:lease,slot:0,request:`{"frozen":true}`}
  var body []byte
  err:=q.QueryRow(context.Background(),watermarkChildSourcesQuery,lease.Expansion,lease.Token,0,q.request,-1).Scan(&body)
  want:=[]string{"tenant","query","commit","rollback"};if scenario=="query"{want=[]string{"tenant","query","rollback"}}
  if !reflect.DeepEqual(tx.events,want)||pool.begins!=1{t.Fatal(tx.events,pool.begins)}
  if scenario=="success"{if err!=nil||string(body)!=`{"items":[],"next":null}`{t.Fatal(string(body),err)}}else if !errors.Is(err,failure){t.Fatal("failure swallowed",err)}
 })}
}

func TestWatermarkSourceQueriesRejectScopeSubstitutionBeforeBegin(t *testing.T){
 tx:=&watermarkSourceQueryTx{};pool:=&watermarkSourceQueryPool{tx:tx}
 lease:=delivery.ExpansionLease{Tenant:uuid.New(),Run:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),ParentTask:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),Until:time.Now().Add(time.Minute),SlotCount:1}
 q:=watermarkChildSourceQueries{db:pool,lease:lease,request:`{"frozen":true}`}
 for _,scenario:=range []string{"sql","expansion","token","slot","request","cursor","type","lease","cancel"}{t.Run(scenario,func(t *testing.T){
  query:=watermarkChildSourcesQuery;args:=[]any{lease.Expansion,lease.Token,0,q.request,-1};ctx:=context.Background();owner:=q
  switch scenario{
  case "sql":query="SELECT arbitrary()"
  case "expansion":args[0]=uuid.New()
  case "token":args[1]=uuid.New()
  case "slot":args[2]=1
  case "request":args[3]=`{"frozen":false}`
  case "cursor":args[4]=1000001
  case "type":args[4]=int64(-1)
  case "lease":owner.lease.Package=uuid.Nil
  case "cancel":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()
  }
  var body []byte;err:=owner.QueryRow(ctx,query,args...).Scan(&body)
  if err==nil||pool.begins!=0||len(tx.events)!=0{t.Fatal("foreign query reached SQL",scenario,err)}
  if scenario=="cancel"&&err!=context.Canceled{t.Fatal(err)}
 })}
}
