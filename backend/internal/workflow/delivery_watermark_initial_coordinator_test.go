package workflow

import (
 "context"
 "errors"
 "reflect"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

type initialWatermarkFixture struct {
 events []string
 open bool
 phase int
 activationReads int
 scenario string
 failure error
}
type initialWatermarkTx struct { pgx.Tx; fixture *initialWatermarkFixture }
type initialWatermarkRow struct { installed bool }
func(r initialWatermarkRow)Scan(dest ...any)error{*dest[0].(*bool)=r.installed;return nil}
func(f *initialWatermarkFixture)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){
 if f.open{panic("overlapping transaction")};f.phase++;f.open=true;f.events=append(f.events,"begin")
 return &initialWatermarkTx{fixture:f},nil
}
func(t *initialWatermarkTx)Exec(_ context.Context,q string,_ ...any)(pgconn.CommandTag,error){
 if q!=`SELECT set_config('app.tenant_id',$1,true)`{panic("unexpected SQL")}
 t.fixture.events=append(t.fixture.events,"tenant");return pgconn.NewCommandTag("SELECT 1"),nil
}
func(t *initialWatermarkTx)QueryRow(context.Context,string,...any)pgx.Row{
 t.fixture.events=append(t.fixture.events,"installation");return initialWatermarkRow{t.fixture.scenario!="installation"}
}
func(t *initialWatermarkTx)Commit(context.Context)error{
 f:=t.fixture;f.events=append(f.events,"commit")
 if f.scenario=="commit"&&f.phase==2{return f.failure};f.open=false;return nil
}
func(t *initialWatermarkTx)Rollback(context.Context)error{t.fixture.open=false;t.fixture.events=append(t.fixture.events,"rollback");return nil}
func(f *initialWatermarkFixture)step(name string)error{
 f.events=append(f.events,name)
 if !f.open&&name!="publish"{panic("admission outside transaction")}
 if f.open&&name=="publish"{panic("object IO inside transaction")}
 if f.scenario==name{return f.failure};return nil
}
func(f *initialWatermarkFixture)DraftTask()(v2input.DraftTask,error){return v2input.DraftTask{},nil}
func(f *initialWatermarkFixture)PublishInputs(context.Context,v2input.Publisher)(v2input.GraphTask,error){return v2input.GraphTask{},f.step("publish")}
func(f *initialWatermarkFixture)CreateDraft(context.Context,delivery.ProofTransaction,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent,uuid.UUID,int64)error{return f.step("draft")}
func(f *initialWatermarkFixture)ActivationRegistered(context.Context,delivery.ProofTransaction,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent,uuid.UUID)(bool,error){
 f.activationReads++;err:=f.step("activated")
 return f.scenario=="recovered"||(f.scenario=="race"&&f.activationReads==2),err
}
func(f *initialWatermarkFixture)TransportRegistered(context.Context,delivery.ProofTransaction,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent)(bool,error){return f.scenario=="registered",f.step("transport")}
func(f *initialWatermarkFixture)RegisterInputsInTransaction(context.Context,pgx.Tx,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent,uuid.UUID,int64,v2input.GraphTask)error{return f.step("register")}
func(f *initialWatermarkFixture)ActivateInTransaction(context.Context,delivery.ProofTransaction,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent,uuid.UUID)error{return f.step("activate")}

func TestInitialWatermarkAdmissionPhases(t *testing.T){
 initial:=[]string{"begin","tenant","installation","commit","rollback"}
 for _,tc:=range []struct{name string;want []string;recovered,failed bool}{
  {"fresh",[]string{"begin","tenant","activated","transport","draft","commit","rollback","publish","begin","tenant","activated","transport","register","activate","commit","rollback"},false,false},
  {"recovered",[]string{"begin","tenant","activated","commit","rollback"},true,false},
  {"registered",[]string{"begin","tenant","activated","transport","commit","rollback","begin","tenant","activated","transport","activate","commit","rollback"},false,false},
  {"race",[]string{"begin","tenant","activated","transport","draft","commit","rollback","publish","begin","tenant","activated","commit","rollback"},false,false},
  {"draft",[]string{"begin","tenant","activated","transport","draft","rollback"},false,true},
  {"publish",[]string{"begin","tenant","activated","transport","draft","commit","rollback","publish"},false,true},
  {"register",[]string{"begin","tenant","activated","transport","draft","commit","rollback","publish","begin","tenant","activated","transport","register","rollback"},false,true},
  {"activate",[]string{"begin","tenant","activated","transport","draft","commit","rollback","publish","begin","tenant","activated","transport","register","activate","rollback"},false,true},
  {"commit",[]string{"begin","tenant","activated","transport","draft","commit","rollback"},false,true},
 }{t.Run(tc.name,func(t *testing.T){
  f:=&initialWatermarkFixture{scenario:tc.name,failure:errors.New(tc.name)}
  child:=delivery.ExportChildIntent{RunID:uuid.New()}
  recovered,err:=admitInitialWatermark(context.Background(),f,deliveryCoordinatorPublisher{},f,delivery.ExpansionLease{Tenant:uuid.New()},delivery.PreparePlan{},child,uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1")),1024)
  if recovered!=tc.recovered||(err!=nil)!=tc.failed{t.Fatal(recovered,err)}
  if tc.failed&&!errors.Is(err,f.failure){t.Fatal("failure not propagated",err)}
  want:=append(append([]string{},initial...),tc.want...)
  if f.open||!reflect.DeepEqual(f.events,want){t.Fatal("phase ordering",f.events,want)}
 })}
}

func TestInitialWatermarkInstallationRefusesBeforeDraftOrIO(t *testing.T){
 f:=&initialWatermarkFixture{scenario:"installation"};child:=delivery.ExportChildIntent{RunID:uuid.New()}
 recovered,err:=admitInitialWatermark(context.Background(),f,deliveryCoordinatorPublisher{},f,delivery.ExpansionLease{Tenant:uuid.New()},delivery.PreparePlan{},child,uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1")),1024)
 if err==nil||recovered||f.open||!reflect.DeepEqual(f.events,[]string{"begin","tenant","installation","rollback"}){t.Fatal(f.events,recovered,err)}
}
