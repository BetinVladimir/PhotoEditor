package v2optical

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
)

func TestNegativeInvalidationSettlementClosedWireAndCounts(t *testing.T) {
 plan:=NegativeInvalidationPlan{AffectedTasks:[]uuid.UUID{uuid.New()},ConsumerRuns:[]uuid.UUID{uuid.New()}}
 valid:=NegativeInvalidationSettlement{SchemaVersion:1,AffectedTasks:1,ConsumerRuns:1,UnsettledConsumerTasks:1}
 for _,tc:=range []struct{name string;edit func(map[string]any);good bool}{
  {"pending unrelated consumer task",func(m map[string]any){},true},
  {"settled",func(m map[string]any){m["unsettled_consumer_tasks"]=0;m["settled"]=true},true},
  {"false settled",func(m map[string]any){m["settled"]=true},false},
  {"lost affected task",func(m map[string]any){m["affected_tasks"]=0},false},
  {"lost run",func(m map[string]any){m["consumer_runs"]=0},false},
  {"negative",func(m map[string]any){m["unsettled_affected_tasks"]=-1},false},
  {"too many",func(m map[string]any){m["unsettled_consumer_runs"]=2},false},
  {"unsafe bound",func(m map[string]any){m["unsettled_consumer_tasks"]=4097},false},
  {"unknown",func(m map[string]any){m["authority"]=true},false},
  {"missing false",func(m map[string]any){delete(m,"settled")},false},
  {"null count",func(m map[string]any){m["unsettled_affected_tasks"]=nil},false},
 } {
  t.Run(tc.name,func(t *testing.T){raw,_:=json.Marshal(valid);var m map[string]any;_ = json.Unmarshal(raw,&m);tc.edit(m);raw,_=json.Marshal(m)
   _,err:=decodeNegativeInvalidationSettlement(raw,plan);if (err==nil)!=tc.good {t.Fatal("settlement acceptance",err)}
  })
 }
 for _,raw:=range []string{"", "null", "{}", "[]", "{broken"} {if _,err:=decodeNegativeInvalidationSettlement([]byte(raw),plan);err==nil {t.Fatal("invalid wire accepted",raw)}}
}

type negativeReconcileDB struct{tx *negativeReconcileTx}
func(d negativeReconcileDB)Begin(context.Context)(pgx.Tx,error){return d.tx,nil}
type negativeReconcileTx struct{negativeRecordTx;body []byte}
func(t *negativeReconcileTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row {
 if q!="SELECT photo.reconcile_v29_negative_invalidation($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)" {return t.negativeRecordTx.QueryRow(ctx,q,args...)}
 t.records++
 return negativeLockRow(func(dest ...any)error{
  if t.commits!=1||args[4]!=t.review||args[5]!=t.hash {return errors.New("wrong reconciliation identity/order")}
  if t.mode=="query_error" {return errors.New("unavailable")}
  *dest[0].(*[]byte)=append([]byte(nil),t.body...);return nil
 })
}
func TestNegativeInvalidationReconcileCommitAndRecoveryBoundary(t *testing.T){
 for _,mode:=range []string{"valid","refused","query_error","commit_error","malformed"} {
  origin,run,tenant,project,review:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
  raw,_:=canonical.Marshal([]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}}})
  body,_:=canonical.Marshal(NegativeInvalidationSettlement{SchemaVersion:1,Settled:true})
  if mode=="refused" {body=[]byte("null")};if mode=="malformed" {body=[]byte(`{"settled":true}`)}
  tx:=&negativeReconcileTx{negativeRecordTx:negativeRecordTx{negativeInventoryTx:negativeInventoryTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},raw:raw},mode:mode,review:review,hash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},body:body}
  plan,status,err:=ReconcileNegativeInvalidation(context.Background(),negativeReconcileDB{tx},auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},tenant,project,review,origin,run,tx.hash,"base",10,10)
  if mode=="valid" {if err!=nil||plan.OriginTask!=origin||!status.Settled||tx.commits!=2 {t.Fatal("valid reconcile",err)}} else if err==nil||plan.OriginTask!=uuid.Nil||status.Settled {t.Fatal("uncommitted observation exposed",mode,err)}
  if tx.records!=1||tx.rollbacks!=2 {t.Fatal("cleanup/order",tx.records,tx.rollbacks)}
 }
}
