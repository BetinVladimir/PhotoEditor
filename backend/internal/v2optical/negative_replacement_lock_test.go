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
type negativeReplacementScopeTx struct {negativeSourceScopeTx;gates int;refuse bool;queryError bool}
func(t *negativeReplacementScopeTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row {
 if q!="SELECT photo.lock_v29_negative_replacement_scope($1,$2,$3,$4,$5,$6,$7::jsonb)" {return t.negativeSourceScopeTx.QueryRow(ctx,q,args...)}
 t.gates++;return negativeLockRow(func(dest ...any)error{if t.queryError {return errors.New("unavailable")};if len(args)!=7||args[0]!=t.tenant {return errors.New("wrong scope")};*dest[0].(*bool)=!t.refuse;return nil})
}
func TestReplacementScopeGateRechecksSQLAndStopsOnRefusal(t *testing.T){
 tenant,project,reviewID,origin,run:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 plan,_:=PlanNegativeInvalidation(context.Background(),origin,run,"base",[]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}}},10,10)
 hash:="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 for _,mode:=range []string{"valid","refused","query_error"}{
  tx:=&negativeReplacementScopeTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},refuse:mode=="refused",queryError:mode=="query_error"}
  err:=LockNegativeReplacementScope(context.Background(),tx,who,tenant,project,reviewID,hash,plan)
  if (err==nil)!=(mode=="valid")||tx.gates!=1 {t.Fatal("gate outcome",mode,err)}
 }
 tx:=&negativeReplacementScopeTx{}
 if err:=LockNegativeReplacementScope(context.Background(),tx,auth.Identity{},tenant,project,reviewID,hash,plan);err!=v2input.ErrInvalid||tx.gates!=0 {t.Fatal("missing identity reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=LockNegativeReplacementScope(ctx,nil,who,tenant,project,reviewID,hash,plan);err!=context.Canceled {t.Fatal(err)}
 if err:=LockOpticalFrozenIntent(context.Background(),nil,who,tenant,project,reviewID,origin,hash,VerifiedOpticalFrozenIntent{});err!=v2input.ErrInvalid {t.Fatal("absent frozen proof",err)}
}
