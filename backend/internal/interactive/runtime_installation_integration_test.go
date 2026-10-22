package interactive

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Authored only; operator supplies an isolated, fully migrated database.
// Catalogue/ACL modifications are transactionally rolled back for every case.
func TestIntegrationInteractiveRuntimeInstallationCurrentSQLAndPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,scenario:=range []string{"installed","caller_bound_discovery","api_execute_revoked","public_execute"}{t.Run(scenario,func(t *testing.T){
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
  switch scenario{
  case "caller_bound_discovery":
   var definition string;if err=tx.QueryRow(ctx,"SELECT pg_get_functiondef('photo.interactive_worker_discoverable(uuid,uuid,text,text,text)'::regprocedure)").Scan(&definition);err!=nil{t.Fatal(err)}
   needle:="AND w.enabled AND w.execution_pool='general'"
   if strings.Count(definition,needle)!=1{t.Fatal("unexpected current capability definition")}
   if _,err=tx.Exec(ctx,strings.Replace(definition,needle,"AND photo.worker_authorized(p_tenant,w.worker_id,op.resource_class,w.node_id,p_operation,p_backend,p_lock)",1));err!=nil{t.Fatal(err)}
  case "api_execute_revoked":_,err=tx.Exec(ctx,"REVOKE EXECUTE ON FUNCTION photo.select_interactive_runtime(uuid,uuid,uuid,uuid,text) FROM photo_api")
  case "public_execute":_,err=tx.Exec(ctx,"GRANT EXECUTE ON FUNCTION photo.interactive_worker_discoverable(uuid,uuid,text,text,text) TO PUBLIC")
  };if err!=nil{t.Fatal(err)}
  if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
  if err=RequireRuntimeInstallation(ctx,tx);(err==nil)!=(scenario=="installed"){t.Fatal(scenario,err)}
 })}
}
