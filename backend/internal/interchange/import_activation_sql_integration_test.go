package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Authored during implementation; run only against an isolated database after
// applying migrations. ACL/absent-scope checks do not prove populated admission.
func TestIntegrationImportActivationAndReceiptPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL with migrations01574/01575 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,signature:=range []string{
  "photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb)",
  "photo.read_v30_interchange_import_activation(uuid,uuid,text,text,text,jsonb)",
  "photo.activate_v30_interchange_import_pre_receipt(uuid,uuid,text,text,text,jsonb)",
 }{
  for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_portal"}{
   var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   expected:=role=="photo_scheduler"&&signature!="photo.activate_v30_interchange_import_pre_receipt(uuid,uuid,text,text,text,jsonb)"
   if allowed!=expected{t.Fatal("unexpected import activation privilege",role,signature,allowed)}
  }
  var secure bool
  if err:=db.QueryRow(ctx,`SELECT p.prosecdef AND owner.rolname='photo_fn_owner'
   AND 'search_path=pg_catalog, photo'=ANY(p.proconfig)
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
   FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&secure);err!=nil{t.Fatal(err)}
  if !secure{t.Fatal("unsafe activation SQL installation",signature)}
 }
}

func TestIntegrationImportActivationRejectsAbsentScopeWithoutTaskCreation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL with migration01575 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tenant,workflow:=uuid.New(),uuid.New();task:=uuid.NewSHA1(workflow,[]byte("v2.interchange.import:0"))
 if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{t.Fatal(err)}
 if _,err:=tx.Exec(ctx,`SET LOCAL ROLE photo_scheduler`);err!=nil{t.Fatal(err)}
 for _,query:=range []string{
  `SELECT photo.read_v30_interchange_import_activation($1,$2,'{}','{}','{}','{}'::jsonb)`,
  `SELECT photo.activate_v30_interchange_import($1,$2,'{}','{}','{}','{}'::jsonb)`,
 }{
  var accepted bool
  if err:=tx.QueryRow(ctx,query,tenant,workflow).Scan(&accepted);err!=nil{t.Fatal(err)}
  if accepted{t.Fatal("absent workflow accepted")}
 }
 if _,err:=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 var count int
 if err:=tx.QueryRow(ctx,`SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND id=$2`,tenant,task).Scan(&count);err!=nil{t.Fatal(err)}
 if count!=0{t.Fatal("activation created a task without prior admission")}
}
