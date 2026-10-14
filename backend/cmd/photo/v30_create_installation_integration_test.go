package main
import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Installation/ACL coverage only; no object writes or lifecycle completion claim.
func TestIntegrationV30CreatePublicationInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,group:=range []struct{role string;signatures []string}{
 {"photo_api",[]string{
 "photo.preflight_v30_delivery_create(uuid,uuid,uuid,uuid,uuid,uuid[])",
 "photo.capture_v30_delivery_draft_selection(uuid,uuid,uuid,uuid,uuid)",
 "photo.resolve_v30_delivery_selection(uuid,uuid,uuid,uuid,uuid,jsonb)",
 "photo.reserve_v30_delivery_create_document(uuid,uuid,uuid,text,text,uuid,text,text,text)",
 "photo.complete_v30_delivery_create_document(uuid,uuid,uuid,text,text,uuid,text,text,text,uuid,text)",
 "photo.reserve_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text)",
 "photo.complete_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text,uuid,text)",
 }},
 {"photo_scheduler",[]string{
 "photo.reserve_v2_input_document(uuid,uuid,uuid,text,text,bigint,text)",
 "photo.complete_v2_input_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)",
 "photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb)",
 "photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb)",
 }},
 }{
  for _,signature:=range group.signatures{
   var secure,owned,public,allowed bool
   err=db.QueryRow(ctx,`SELECT p.prosecdef,owner.rolname='photo_fn_owner',
    EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'),
    has_function_privilege($2,p.oid,'EXECUTE')
    FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature,group.role).Scan(&secure,&owned,&public,&allowed)
   if err!=nil{t.Fatal(signature,err)}
   if !secure||!owned||public||!allowed{t.Fatal("unsafe or unavailable installation",signature,group.role)}
  }
 }
}
