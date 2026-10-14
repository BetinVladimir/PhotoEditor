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
func TestAssemblyTenantTransactionCommitsOnlySuccessfulAdmission(t *testing.T){
 l:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now(),Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),SlotCount:1}
 failure:=errors.New("admission refused")
 for _,refused:=range []bool{false,true}{
  tx:=&watermarkPendingTx{}
  err:=assemblyTenantTransaction(context.Background(),watermarkPendingPool{tx},l,func(_ context.Context,got pgx.Tx)error{
   if got!=tx{t.Fatal("wrong transaction")}
   tx.events=append(tx.events,"admission")
   if refused{return failure};return nil
  })
  want:=[]string{"tenant","admission","commit","rollback"}
  if refused{want=[]string{"tenant","admission","rollback"}}
  if !reflect.DeepEqual(tx.events,want)||(refused&&!errors.Is(err,failure))||(!refused&&err!=nil){t.Fatal(err,tx.events)}
 }
 db:=&runtimePlanUnexpectedDB{}
 if err:=assemblyTenantTransaction(context.Background(),db,delivery.ExpansionLease{},func(context.Context,pgx.Tx)error{return nil});err==nil||db.calls!=0{t.Fatal(err,db.calls)}
}
