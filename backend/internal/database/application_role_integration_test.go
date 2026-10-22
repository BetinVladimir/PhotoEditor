package database

import (
 "context"
 "os"
 "testing"
 "time"
 "fmt"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationRoleGuardChecksEveryPhysicalConnectionAfterInitializer(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL");if dsn==""{t.Skip("isolated nonprivileged runtime login required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 for _,reset:=range []bool{false,true}{
  config,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)}
  config.ConnConfig.RuntimeParams["role"]="photo_api";config.MaxConns=2
  calls:=0
  config.AfterConnect=func(ctx context.Context,connection *pgx.Conn)error{
   calls++
   if reset{_,err:=connection.Exec(ctx,"SET ROLE photo_scheduler");return err};return nil
  }
  GuardApplicationRole(config)
  pool,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil{t.Fatal(err)}
  if reset{
   if err=pool.Ping(ctx);err==nil{pool.Close();t.Fatal("initializer role switch bypassed exact role gate")}
  }else{
   first,err:=pool.Acquire(ctx);if err!=nil{pool.Close();t.Fatal(err)}
   second,err:=pool.Acquire(ctx);if err!=nil{first.Release();pool.Close();t.Fatal(err)}
   if calls!=2{t.Error("every physical connection must run initializer and guard",calls)}
   first.Release();second.Release()
  }
  pool.Close()
 }
}

func TestIntegrationPrivilegedLoginCannotHideBehindSetRole(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated privileged migration login required")}
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 config,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)}
 config.ConnConfig.RuntimeParams["role"]="photo_api"
 GuardApplicationRole(config)
 pool,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil{t.Fatal(err)};defer pool.Close()
 if err=pool.Ping(ctx);err==nil{t.Fatal("privileged migration login accepted as runtime identity")}
}

// All installation mutations are transaction-local and roll back. The source
// fixture is an isolated migration/admin login, never production credentials.
func TestIntegrationRuntimeCannotAssumeOwnerOrRoleAdministrator(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated privileged migration login required")}
 for index,mutation:=range []string{
  "GRANT photo_runtime_owner TO %s",
  "GRANT photo_fn_owner TO %s",
  "ALTER ROLE %s CREATEROLE",
  "ALTER ROLE %s REPLICATION",
  "GRANT CREATE ON SCHEMA photo TO %s",
  "ALTER SCHEMA photo OWNER TO %s",
  "ALTER TABLE photo.projects OWNER TO %s",
  "ALTER FUNCTION photo.authorize_tenant(uuid,uuid,uuid) OWNER TO %s",
 }{
  t.Run(fmt.Sprint(index),func(t *testing.T){
   ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
   connection,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer connection.Close(context.Background())
   tx,err:=connection.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
   name:=fmt.Sprintf("photo_acl_probe_%d_%d",time.Now().UnixNano(),index)
   quoted:=pgx.Identifier{name}.Sanitize()
   for _,sql:=range []string{"CREATE ROLE "+quoted+" NOLOGIN NOINHERIT NOBYPASSRLS", "GRANT photo_api TO "+quoted,
    fmt.Sprintf(mutation,quoted),"SET LOCAL SESSION AUTHORIZATION "+quoted,"SET LOCAL ROLE photo_api"}{
    if _,err=tx.Exec(ctx,sql);err!=nil{t.Fatal("isolated role fixture unavailable",err)}
   }
   if err=RequireApplicationRole(ctx,tx,"photo_api");err==nil{t.Fatal("owner/admin privilege was accepted",mutation)}
  })
 }
}
