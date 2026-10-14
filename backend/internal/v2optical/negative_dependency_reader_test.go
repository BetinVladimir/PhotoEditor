package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
)
type negativeInventoryDB struct{ tx *negativeInventoryTx }
func(d negativeInventoryDB)Begin(context.Context)(pgx.Tx,error){return d.tx,nil}
type negativeInventoryTx struct{ negativeSourceScopeTx; raw []byte;commits int }
func(t *negativeInventoryTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row {
 if q=="SELECT photo.read_v29_negative_dependency_inventory($1,$2,$3,$4,$5,$6,$7)" {
  t.calls=append(t.calls,q)
  return negativeLockRow(func(dest ...any)error{*dest[0].(*[]byte)=append([]byte(nil),t.raw...);return nil})
 }
 return t.negativeSourceScopeTx.QueryRow(ctx,q,args...)
}
func(t *negativeInventoryTx)Commit(context.Context)error{t.commits++;return nil}
func(t *negativeInventoryTx)Rollback(context.Context)error{return nil}
func TestNegativeDependencyReaderRejectsUntrustedInventoryBeforeCommit(t *testing.T) {
 origin,run,tenant,project:=uuid.New(),uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 for _,mode:=range []string{"valid","missing","unknown","cyclic"} {
  raw,_:=canonical.Marshal([]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}}})
  if mode=="missing" { raw=nil };if mode=="unknown" { raw=[]byte(`[{"task_id":"`+origin.String()+`","run_id":"`+run.String()+`","parents":[],"extra":true}]`) }
  if mode=="cyclic" { raw,_=canonical.Marshal([]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{origin}}}) }
  tx:=&negativeInventoryTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},raw:raw}
  plan,err:=ReadNegativeDependencyPlan(context.Background(),negativeInventoryDB{tx},who,tenant,project,origin,run,"base",10,10)
  if mode=="valid" { if err!=nil||plan.OriginTask!=origin||tx.commits!=1 { t.Fatal("valid inventory",plan,err) } } else if err==nil||tx.commits!=0 { t.Fatal("invalid inventory committed",mode,err) }
 }
}
