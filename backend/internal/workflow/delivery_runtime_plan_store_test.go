package workflow

import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/delivery"
)
func TestRuntimePlanStoreRefusesMissingTransactionsAndCancellation(t *testing.T){
 var store DeliveryRuntimePlanStore
 if _,err:=store.LoadPlan(context.Background(),delivery.ExpansionLease{});err==nil{t.Fatal("missing authority DB accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=store.LoadPlan(ctx,delivery.ExpansionLease{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
}

// A malformed lease must not open even the installation-check transaction.
type runtimePlanUnexpectedDB struct{ calls int }
func(d *runtimePlanUnexpectedDB)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){
 d.calls++
 return nil,v2input.ErrInvalid
}
func TestRuntimePlanStoreRejectsMalformedLeaseBeforeDatabase(t *testing.T){
 valid:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now(),Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),SlotCount:1}
 cases:=map[string]func(*delivery.ExpansionLease){
  "expansion":func(l *delivery.ExpansionLease){l.Expansion=uuid.Nil},
  "token":func(l *delivery.ExpansionLease){l.Token=uuid.Nil},
  "until":func(l *delivery.ExpansionLease){l.Until=time.Time{}},
  "tenant":func(l *delivery.ExpansionLease){l.Tenant=uuid.Nil},
  "run":func(l *delivery.ExpansionLease){l.Run=uuid.Nil},
  "workflow":func(l *delivery.ExpansionLease){l.Workflow=uuid.Nil},
  "package":func(l *delivery.ExpansionLease){l.Package=uuid.Nil},
  "parent":func(l *delivery.ExpansionLease){l.ParentTask=uuid.Nil},
  "empty slots":func(l *delivery.ExpansionLease){l.SlotCount=0},
  "too many slots":func(l *delivery.ExpansionLease){l.SlotCount=80001},
  "negative cursor":func(l *delivery.ExpansionLease){l.Cursor=-1},
  "cursor past slots":func(l *delivery.ExpansionLease){l.Cursor=2},
 }
 for name,mutate:=range cases{t.Run(name,func(t *testing.T){
  lease:=valid;mutate(&lease);db:=&runtimePlanUnexpectedDB{}
  _,err:=(DeliveryRuntimePlanStore{Transactions:db}).LoadPlan(context.Background(),lease)
  if err!=v2input.ErrInvalid||db.calls!=0{t.Fatal("invalid lease reached database",err,db.calls)}
 })}
}
