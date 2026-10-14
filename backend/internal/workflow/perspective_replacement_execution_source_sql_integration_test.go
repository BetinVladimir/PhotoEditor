package workflow

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Installation/ACL coverage only; does not establish positive replacement replay.
func TestIntegrationPerspectiveReplacementExecutionSourceInstallation(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn=="" { t.Skip("isolated migrated database required") }
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil { t.Fatal(err) };defer db.Close()
 for _,signature:=range []string{
  "photo.v29_perspective_execution_session(jsonb)",
  "photo.v29_perspective_execution_source_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb)",
 } {
  var owned,public bool
  err=db.QueryRow(ctx,`SELECT r.rolname='photo_fn_owner',EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&owned,&public)
  if err!=nil { t.Fatal(err) };if !owned||public { t.Fatal("private source helper exposed",signature) }
  for _,role:=range []string{"photo_worker","photo_scheduler","photo_api","photo_gateway","photo_portal"} {
   var allowed bool
   if err=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,signature).Scan(&allowed);err!=nil { t.Fatal(err) }
   if allowed { t.Fatal("private helper executable",signature,role) }
  }
 }
 for _,signature:=range []string{
  "photo.v29_worker_perspective_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)",
  "photo.v29_perspective_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)",
 } {
  var routed bool
  if err=db.QueryRow(ctx,`SELECT strpos(pg_get_functiondef(to_regprocedure($1)),'v29_perspective_execution_source_scope')>0 AND strpos(pg_get_functiondef(to_regprocedure($1)),'v29_perspective_execution_session')>0`,signature).Scan(&routed);err!=nil { t.Fatal(err) }
  if !routed { t.Fatal("replacement execution source not installed",signature) }
 }
 tx,err:=db.Begin(ctx);if err!=nil { t.Fatal(err) };defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil { t.Fatal(err) }
 var installed bool
 if err=tx.QueryRow(ctx,perspectiveReplacementExecutionInstallationSQL).Scan(&installed);err!=nil { t.Fatal(err) }
 if !installed { t.Fatal("complete execution installation refused") }

}
