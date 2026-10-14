package workflow

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
)

// Requires an isolated, fully migrated operator-owned test database. Every
// catalog mutation is transaction-local and rolled back, even on assertion failure.
func TestIntegrationCapacitySchedulerContractCatalogDrift(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated operator database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 for _,signature:=range capacitySchedulerSignatures{
  for _,mode:=range []string{"baseline","missing","api","worker","public","scheduler","owner","invoker","search-path","unset-search-path"}{
   t.Run(signature+"/"+mode,func(t *testing.T){
    tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
    // Signatures are fixed source constants, never external SQL input.
    statement:=""
    switch mode{
    case "missing":statement="ALTER FUNCTION "+signature+" RENAME TO capacity_contract_missing_probe"
    case "api":statement="GRANT EXECUTE ON FUNCTION "+signature+" TO photo_api"
    case "worker":statement="GRANT EXECUTE ON FUNCTION "+signature+" TO photo_worker"
    case "public":statement="GRANT EXECUTE ON FUNCTION "+signature+" TO PUBLIC"
    case "scheduler":statement="REVOKE EXECUTE ON FUNCTION "+signature+" FROM photo_scheduler"
    case "owner":statement="ALTER FUNCTION "+signature+" OWNER TO photo_maintenance"
    case "invoker":statement="ALTER FUNCTION "+signature+" SECURITY INVOKER"
    case "search-path":statement="ALTER FUNCTION "+signature+" SET search_path=public"
    case "unset-search-path":statement="ALTER FUNCTION "+signature+" RESET ALL"
    }
    if statement!=""{if _,err=tx.Exec(ctx,statement);err!=nil{t.Fatal(err)}}
    if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
    err=CheckCapacitySchedulerContract(ctx,tx)
    if (err==nil)!=(mode=="baseline"){t.Fatal("incorrect catalog readiness",mode,err)}
   })
  }
 }
 for _,signature:=range capacitySchedulerPrivateSignatures{
  for _,mode:=range []string{"baseline","missing","scheduler-grant","api-grant","worker-grant","owner","invoker","search-path"}{
   t.Run(signature+"/"+mode,func(t *testing.T){
    tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
    statement:=""
    switch mode{
    case "missing":statement="ALTER FUNCTION "+signature+" RENAME TO capacity_private_missing_probe"
    case "scheduler-grant":statement="GRANT EXECUTE ON FUNCTION "+signature+" TO photo_scheduler"
    case "api-grant":statement="GRANT EXECUTE ON FUNCTION "+signature+" TO photo_api"
    case "worker-grant":statement="GRANT EXECUTE ON FUNCTION "+signature+" TO photo_worker"
    case "owner":statement="ALTER FUNCTION "+signature+" OWNER TO photo_maintenance"
    case "invoker":statement="ALTER FUNCTION "+signature+" SECURITY INVOKER"
    case "search-path":statement="ALTER FUNCTION "+signature+" SET search_path=public"
    }
    if statement!=""{if _,err=tx.Exec(ctx,statement);err!=nil{t.Fatal(err)}}
    if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
    err=CheckCapacitySchedulerContract(ctx,tx)
    if (err==nil)!=(mode=="baseline"){t.Fatal("incorrect private catalog readiness",mode,err)}
   })
  }
 }
}
