package v2optical

import (
 "context"
 "os"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

// ACL evidence only. Does not prove quota/publication/recovery lifecycle.
func TestIntegrationReplacementDetectorPendingAdoptionPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,fn:=range []struct{signature string;private bool}{
 {"photo.v2_pending_adoptions_pre_v29_replacement_detector()",true},
 {"photo.next_v29_replacement_detector_adoption(uuid)",false},
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
}
