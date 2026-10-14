package v2optical

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Installation/denied scope only; full positive publication is separate coverage.
func TestIntegrationMultiReplacementIdentityGateIsolation(t *testing.T){
 testMultiReplacementGateIsolation(t,"lock_v29_multi_replacement_identity")
 testMultiReplacementGateIsolation(t,"lock_v29_multi_replacement_inventory")
 testMultiReplacementGateIsolation(t,"save_v29_multi_replacement_draft")
 testMultiReplacementGateIsolation(t,"read_v29_multi_replacement_draft")
}
func testMultiReplacementGateIsolation(t *testing.T,name string){
 t.Helper()
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo."+name+"(uuid,uuid,uuid,uuid,uuid,text,jsonb,text)"
 var secure,owned,public bool
 if err=db.QueryRow(ctx,`SELECT p.prosecdef,r.rolname='photo_fn_owner',EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&secure,&owned,&public);err!=nil{t.Fatal(err)}
 if !secure||!owned||public{t.Fatal("unsafe multi identity gate")}
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_portal"}{
  var allowed bool;if err=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected gate privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
 tenant:=uuid.New();if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{t.Fatal(err)}
 for _,raw:=range []string{"",`{}`,`{"schema_version":2,"identity":{},"draft":{}}`}{
  var ready bool
  row:=tx.QueryRow(ctx,"SELECT photo."+name+"($1,$2,$3,$4,$5,$6,'{}'::jsonb,$7)",tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),"invalid",raw)
  if name=="read_v29_multi_replacement_draft"{
   var stored *string;if err=row.Scan(&stored);err!=nil{t.Fatal(err)};ready=stored!=nil
  }else if err=row.Scan(&ready);err!=nil{t.Fatal(err)}
  if ready{t.Fatal("absent scope accepted")}
 }
}
