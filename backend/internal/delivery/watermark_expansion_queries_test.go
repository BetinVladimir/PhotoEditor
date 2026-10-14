package delivery
import (
 "context"
 "errors"
 "reflect"
 "testing"
 "github.com/google/uuid"
)
func TestWatermarkExpansionQueriesBindLeaseAndCommit(t *testing.T){
 for _,mode:=range []string{"valid","scan","commit","cancel"}{
  lease:=validExpansionLease();tx:=&metadataQueryTx{};pool:=&metadataQueryPool{tx:tx};queries:=watermarkExpansionQueries{db:pool,lease:lease}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  if mode=="scan"{tx.scanErr=errors.New("scan")};if mode=="commit"{tx.commitErr=errors.New("commit")};if mode=="cancel"{tx.cancel=cancel}
  var ready bool;err:=queries.QueryRow(ctx,watermarkExpansionPlanQuery,lease.Expansion,lease.Token).Scan(&ready)
  if (err==nil)!=(mode=="valid"){t.Fatal(mode,err)}
  expected:=[]string{"tenant","query","rollback"};if mode=="valid"||mode=="commit"{expected=[]string{"tenant","query","commit","rollback"}}
  if !reflect.DeepEqual(tx.events,expected){t.Fatal(mode,tx.events)}
 }
 lease:=validExpansionLease();pool:=&metadataQueryPool{tx:&metadataQueryTx{}};queries:=watermarkExpansionQueries{db:pool,lease:lease};var ready bool
 for _,input:=range []struct{query string;args []any}{{"SELECT true",nil},{watermarkExpansionPlanQuery,[]any{lease.Expansion,uuid.New()}},{watermarkExpansionScopeQuery,[]any{uuid.New(),lease.Run,lease.Expansion,lease.Token,lease.ParentTask}},{watermarkPinReadQuery,[]any{lease.Tenant,lease.Run,lease.Expansion,lease.Token,uuid.New(),"invalid"}}}{if err:=queries.QueryRow(context.Background(),input.query,input.args...).Scan(&ready);err==nil||pool.begins!=0{t.Fatal("query/scope bypass")}}
}
