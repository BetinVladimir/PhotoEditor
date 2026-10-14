package v2render

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Privilege and absent/foreign authority cases; not a positive admission lifecycle.
func TestIntegrationMultiReplacementReviewAPIIsolation(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn=="" { t.Skip("isolated migrated database required") }
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil { t.Fatal(err) };defer db.Close()
 for _,signature:=range []string{
  "photo.read_v29_multi_replacement_review_scope(uuid,uuid,uuid,uuid,uuid,text,uuid,text)",
  "photo.list_v29_multi_replacement_reviews(uuid,uuid,uuid,uuid,text,uuid)",
 } {
  var secure,owned,public bool
  if err=db.QueryRow(ctx,`SELECT p.prosecdef,r.rolname='photo_fn_owner',EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&secure,&owned,&public);err!=nil { t.Fatal(err) }
  if !secure||!owned||public { t.Fatal("unsafe API reader",signature) }
  for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal","photo_auth","photo_publisher","photo_maintenance"} {
   var allowed bool;if err=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,signature).Scan(&allowed);err!=nil { t.Fatal(err) }
   if allowed!=(role=="photo_api") { t.Fatal("wrong reader ACL",signature,role) }
  }
 }
 tx,err:=db.Begin(ctx);if err!=nil { t.Fatal(err) };defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil { t.Fatal(err) }
 current,foreign:=uuid.New(),uuid.New()
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",current.String());err!=nil { t.Fatal(err) }
 for _,tenantID:=range []uuid.UUID{current,foreign} {
  var raw []byte
  if err=tx.QueryRow(ctx,"SELECT photo.list_v29_multi_replacement_reviews($1,$2,$3,$4,$5,NULL)",tenantID,uuid.New(),uuid.New(),uuid.New(),"bad").Scan(&raw);err!=nil { t.Fatal(err) }
  if len(raw)!=0 { t.Fatal("absent/foreign review disclosed") }
  for _,operation:=range []string{"optical.panorama","optical.focus_stack","optical.perspective",""}{
   if err=tx.QueryRow(ctx,"SELECT photo.read_v29_multi_replacement_review_scope($1,$2,$3,$4,$5,$6,$7,$8)",tenantID,uuid.New(),uuid.New(),uuid.New(),uuid.New(),"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",uuid.New(),operation).Scan(&raw);err!=nil{t.Fatal(err)}
   if len(raw)!=0{t.Fatal("absent/foreign replacement scope disclosed",operation)}
  }
 }
}
