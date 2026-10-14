package v2optical

import (
 "context"
 "os"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// ACL/invalid-scope coverage only; full positive physical staging/quota/replay
// must be exercised in the deferred deep integration phase.
func TestIntegrationReplacementNegativeStagePrivilegesAndInvalidScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,fn:=range []struct{signature string;private bool}{
  {"photo.stage_v29_negative_replacement_task(uuid,uuid,text,jsonb)",false},
  {"photo.v2_input_document_scope_pre_v29_negative(uuid,uuid,uuid,text)",true},
  {"photo.register_v2_task_input_batch_pre_v29_negative(uuid,uuid,uuid,integer,jsonb)",true},
  {"photo.seal_v2_task_inputs_pre_v29_negative(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb)",true},
  {"photo.seal_v2_graph_pre_v29_negative(uuid,uuid)",true},
 }{
  for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_auth","photo_publisher","photo_maintenance","photo_portal"}{
   var allowed bool
   if err:=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,fn.signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_scheduler"&&!fn.private){t.Fatal("staging authority exposed",fn.signature,role,allowed)}
  }
 }
 for _,descriptor:=range []string{"{}","null","[]","{bad"}{
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)}
  tenant:=uuid.New();var staged bool
  if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{tx.Rollback(ctx);t.Fatal(err)}
  err=tx.QueryRow(ctx,"SELECT photo.stage_v29_negative_replacement_task($1,$2,$3,'{}'::jsonb)",tenant,uuid.New(),descriptor).Scan(&staged)
  tx.Rollback(ctx)
  if err!=nil||staged{t.Fatal("invalid staging scope not safely refused",descriptor,err)}
 }
}
