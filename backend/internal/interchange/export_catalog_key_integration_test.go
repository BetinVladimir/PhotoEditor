package interchange

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Installation checks preserve security properties and require the repaired
// storage column in each current definition. Positive export lifecycle is separate.
func TestIntegrationExportCatalogUsesActualImmutableObjectKey(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 var columnExists bool
 if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_attribute WHERE attrelid='photo.artifacts'::regclass AND attname='object_key' AND NOT attisdropped)` ).Scan(&columnExists);err!=nil||!columnExists{t.Fatal("immutable object_key missing",err)}
 targets:=map[string]int{
  "photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)":3,
  "photo.v30_interchange_export_output(uuid,uuid,uuid,text,text,uuid,text,text)":1,
  "photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)":1,
 }
 for signature,count:=range targets{
  var definition,owner string;var definer,api,public bool
  err:=tx.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),pg_get_userbyid(p.proowner),p.prosecdef,
   has_function_privilege('photo_api',p.oid,'EXECUTE'),
   EXISTS(SELECT FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
   FROM pg_proc p WHERE p.oid=$1::regprocedure`,signature).Scan(&definition,&owner,&definer,&api,&public)
  if err!=nil{t.Fatal(err)}
  if owner!="photo_fn_owner"||!definer||!api||public{t.Fatalf("export catalogue ACL changed: %s",signature)}
  if strings.Contains(definition,"a.key")||strings.Count(definition,"a.object_key")!=count{t.Fatalf("wrong immutable storage key: %s",signature)}
  if strings.Contains(signature,"export_document(")||strings.Contains(signature,"export_result_input("){
   if !strings.Contains(definition,"'DescriptorShard','FaceObservations','StyleBackgroundAsset'"){t.Fatal("descriptor faces missing from current catalogue",signature)}
  }
  for _,role:=range []string{"photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
   var allowed bool
   if err:=tx.QueryRow(ctx,`SELECT has_function_privilege($1,$2::regprocedure,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed{t.Fatalf("unexpected catalogue execution by %s",role)}
  }
 }
}
