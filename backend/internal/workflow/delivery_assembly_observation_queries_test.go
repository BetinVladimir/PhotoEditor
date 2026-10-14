package workflow

import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
func TestAssemblyObservationQueriesRejectForeignAndUnsupportedBeforeTransaction(t *testing.T){
 l:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now(),Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),Cursor:2,SlotCount:2}
 for _,test:=range []struct{query string;args []any}{
  {`SELECT photo.v30_delivery_observation_page($1,$2,$3,$4)`,[]any{uuid.New(),l.Token,0,1}},
  {`SELECT photo.v30_delivery_observation_page($1,$2,$3,$4)`,[]any{l.Expansion,uuid.New(),0,1}},
  {`SELECT photo.v30_delivery_observation_page($1,$2,$3,$4)`,[]any{l.Expansion,l.Token,-1,1}},
  {`SELECT photo.v30_delivery_observation_page($1,$2,$3,$4)`,[]any{l.Expansion,l.Token,3,1}},
  {`SELECT photo.v30_delivery_observation_page($1,$2,$3,$4)`,[]any{l.Expansion,l.Token,0,101}},
  {`SELECT photo.update_v30_delivery_expansion_lease($1,$2,$3)`,[]any{l.Expansion,l.Token,true}},
  {`SELECT photo.fail_v30_delivery($1,$2,$3)`,[]any{l.Expansion,l.Token,0}},
 }{
  db:=&runtimePlanUnexpectedDB{};q:=deliveryAssemblyObservationQueries{db:db,lease:l}
  var raw []byte
  if err:=q.QueryRow(context.Background(),test.query,test.args...).Scan(&raw);err!=v2input.ErrInvalid||db.calls!=0{t.Fatal(test.query,err,db.calls)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(deliveryAssemblyObservationQueries{}).QueryRow(ctx,"unknown").Scan();err!=context.Canceled{t.Fatal(err)}
}
