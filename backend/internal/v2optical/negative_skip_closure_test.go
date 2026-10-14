package v2optical

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type negativeSkipDB struct {tx *negativeSkipTx}
func(d negativeSkipDB)Begin(context.Context)(pgx.Tx,error){return d.tx,nil}
type negativeSkipTx struct {negativeSourceScopeTx;mode string;service bool;closes,commits,rollbacks int}
func(t *negativeSkipTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 if q!="SELECT photo.close_v29_negative_skipped_dependencies($1,$2,$3,$4,$5,$6,$7::jsonb)" {return t.negativeSourceScopeTx.QueryRow(ctx,q,args...)}
 t.closes++;return negativeLockRow(func(dest ...any)error{
  if len(args)!=7||args[0]!=t.tenant||(t.service&&(args[2]!=nil||args[3]!=nil))||(!t.service&&(args[2]==nil||args[3]==nil)) {return errors.New("wrong closure authority")}
  if t.mode=="query_error" {return errors.New("unavailable")};*dest[0].(*bool)=t.mode!="refused";return nil
 })
}
func(t *negativeSkipTx)Commit(context.Context)error{if t.mode=="commit_error" {return errors.New("ambiguous closure commit")};t.commits++;return nil}
func(t *negativeSkipTx)Rollback(context.Context)error{t.rollbacks++;return nil}
func TestSkipClosureRequiresAuthoritativeSuccessAndPropagatesCommitAmbiguity(t *testing.T){
 tenant,project,reviewID,origin,run:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 plan,_:=PlanNegativeInvalidation(context.Background(),origin,run,"skip",[]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}}},10,10);hash:="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 for _,service:=range []bool{false,true} {for _,mode:=range []string{"valid","refused","query_error","commit_error"}{
  tx:=&negativeSkipTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},mode:mode,service:service}
  err:=closeNegativeSkippedDependencies(context.Background(),negativeSkipDB{tx},who,tenant,project,reviewID,hash,plan,service)
  if (err==nil)!=(mode=="valid")||tx.closes!=1||tx.rollbacks!=1||((tx.commits==1)!=(mode=="valid")) {t.Fatal("closure outcome",mode,err)}
 }}
 plan.Route="base";tx:=&negativeSkipTx{}
 if err:=CloseNegativeSkippedDependencies(context.Background(),negativeSkipDB{tx},who,tenant,project,reviewID,hash,plan);err!=v2input.ErrInvalid||tx.closes!=0 {t.Fatal("base closed as skip",err)}
}
