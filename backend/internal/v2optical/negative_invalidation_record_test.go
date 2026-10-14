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
type negativeRecordDB struct{tx *negativeRecordTx}
func(d negativeRecordDB)Begin(context.Context)(pgx.Tx,error){return d.tx,nil}
type negativeRecordTx struct{negativeInventoryTx;mode string;cancel bool;records,rollbacks int;review uuid.UUID;hash string}
func(t *negativeRecordTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row {
 expected:="SELECT photo.record_v29_negative_invalidation($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)"
 if t.cancel { expected="SELECT photo.request_v29_negative_invalidation_cancel($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)" }
 if q!=expected { return t.negativeInventoryTx.QueryRow(ctx,q,args...) }
 t.records++
 return negativeLockRow(func(dest ...any)error {
  if t.commits!=1||args[4]!=t.review||args[5]!=t.hash { return errors.New("record before inventory commit or lost decision identity") }
  var plan NegativeInvalidationPlan;if json.Unmarshal([]byte(args[6].(string)),&plan)!=nil||plan.Route!="base" { return errors.New("wrong plan") }
  if t.mode=="query_error" { return errors.New("dependency unavailable") }
  *dest[0].(*bool)=t.mode!="refused";return nil
 })
}
func(t *negativeRecordTx)Commit(context.Context)error { if t.mode=="commit_error"&&t.records>0 { return errors.New("ambiguous record commit") };t.commits++;return nil }
func(t *negativeRecordTx)Rollback(context.Context)error { t.rollbacks++;return nil }
func TestNegativeInvalidationRecordRepeatsDecisionAndPropagatesAmbiguousCommit(t *testing.T) {
 for _,cancelConsumers:=range []bool{false,true} {
 for _,mode:=range []string{"valid","refused","query_error","commit_error"} {
  origin,run,tenant,project,review:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
  raw,_:=canonical.Marshal([]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}}})
  tx:=&negativeRecordTx{negativeInventoryTx:negativeInventoryTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},raw:raw},mode:mode,cancel:cancelConsumers,review:review,hash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
  plan,err:=writeNegativeInvalidation(context.Background(),negativeRecordDB{tx},auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},tenant,project,review,origin,run,tx.hash,"base",10,10,cancelConsumers)
  if mode=="valid" { if err!=nil||plan.OriginTask!=origin||tx.commits!=2 { t.Fatal("valid record",err) } } else if err==nil||plan.OriginTask!=uuid.Nil { t.Fatal("failure hidden",mode,err) }
  if tx.records!=1||tx.rollbacks!=2 { t.Fatal("stage count/cleanup",tx.records,tx.rollbacks) }
 }
}
}
