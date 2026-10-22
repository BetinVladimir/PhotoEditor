package capability

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/database"
)

// Authored only; operator supplies an isolated, fully migrated database.
// Catalogue/ACL modifications are transactionally rolled back for every case.
func TestIntegrationCapabilityInstallationCurrentSQLAndPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,scenario:=range []string{"installed","missing_pool_predicate","api_execute_revoked","public_execute"}{t.Run(scenario,func(t *testing.T){
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
  switch scenario{
  case "missing_pool_predicate":
   var definition string;if err=tx.QueryRow(ctx,"SELECT pg_get_functiondef('photo.tenant_capabilities()'::regprocedure)").Scan(&definition);err!=nil{t.Fatal(err)}
   needle:="w.execution_pool='signer' AND w.signer_qualified_until>clock_timestamp()"
   if strings.Count(definition,needle)!=1{t.Fatal("unexpected current capability definition")}
   if _,err=tx.Exec(ctx,strings.Replace(definition,needle,"w.execution_pool='signer'",1));err!=nil{t.Fatal(err)}
  case "api_execute_revoked":_,err=tx.Exec(ctx,"REVOKE EXECUTE ON FUNCTION photo.tenant_capabilities() FROM photo_api")
  case "public_execute":_,err=tx.Exec(ctx,"GRANT EXECUTE ON FUNCTION photo.tenant_capabilities() TO PUBLIC")
  };if err!=nil{t.Fatal(err)}
  if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
  if err=RequireExecutionPoolInstallation(ctx,tx);(err==nil)!=(scenario=="installed"){t.Fatal(scenario,err)}
 })}
}

// Uses a real nonprivileged login separately from transactional admin DDL probes.
func TestIntegrationCapabilityInstallationActualRuntimeCaller(t *testing.T) {
 if os.Getenv("PHOTO_TEST_DATABASE_URL")=="" { t.Skip("isolated migrated PostgreSQL required") }
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 runtimeDSN:=os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL")
 if runtimeDSN=="" { t.Fatal("isolated nonprivileged API runtime connection required") }
 config,err:=pgxpool.ParseConfig(runtimeDSN);if err!=nil { t.Fatal(err) }
 config.ConnConfig.RuntimeParams["role"]="photo_api";database.GuardApplicationRole(config)
 runtime,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil { t.Fatal(err) };defer runtime.Close()
 if err=RequireExecutionPoolInstallation(ctx,runtime);err!=nil { t.Fatal(err) }
}
