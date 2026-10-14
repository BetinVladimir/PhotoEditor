package v2optical

import (
 "context"
 "os"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

// ACL evidence only. Does not prove quota/publication/recovery lifecycle.
func TestIntegrationAcceptedMultiParentPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,fn:=range []struct{signature string;private bool}{
 {"photo.v29_accepted_multi_parent(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text)",true},
 {"photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid)",false},
 }{
  var secure,owned,public bool
  if err:=db.QueryRow(ctx,`SELECT p.prosecdef,r.rolname='photo_fn_owner',EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,fn.signature).Scan(&secure,&owned,&public);err!=nil{t.Fatal(err)}
  if !secure||!owned||public{t.Fatal("unsafe publication function ownership",fn.signature)}
  for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_auth","photo_publisher","photo_maintenance","photo_portal"}{
   var allowed bool
   if err:=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,fn.signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_scheduler"&&!fn.private){t.Fatal("publication authority exposed",fn.signature,role,allowed)}
  }
 }
 for _,operation:=range []string{"optical.panorama","optical.focus_stack","optical.perspective",""}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT photo.v29_accepted_multi_parent(
   '10000000-0000-4000-8000-000000000001','10000000-0000-4000-8000-000000000002',
   '10000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000004',
   '10000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000006',
   '10000000-0000-4000-8000-000000000007',repeat('a',64),
   '10000000-0000-4000-8000-000000000008',repeat('b',64),$1)`,operation).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed{t.Fatal("absent tenant/Apply authority accepted",operation)}
 }
 var routed bool
 if err:=db.QueryRow(ctx,`SELECT strpos(pg_get_functiondef('photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid)'::regprocedure),'v29_accepted_multi_parent')>0`).Scan(&routed);err!=nil{t.Fatal(err)}
 if !routed{t.Fatal("multi accepted parent gate not installed")}
}
