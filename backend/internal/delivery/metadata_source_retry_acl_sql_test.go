package delivery

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationMetadataRetryReaderRoleIsolation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through01717 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,entry:=range []struct{signature string;api bool}{
  {"photo.read_v30_metadata_source_retry(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text[])",true},
  {"photo.v30_metadata_source_reauthorized(uuid,uuid,uuid,uuid,jsonb,text,text,jsonb,text,text,text[])",false},
 }{
  var public bool
  if err=db.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE p.oid=$1::regprocedure AND acl.grantee=0 AND acl.privilege_type='EXECUTE')`,entry.signature).Scan(&public);err!=nil||public{t.Fatal(entry.signature,"PUBLIC",err)}
  for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal"}{
   var allowed bool
   if err=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,entry.signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_api"&&entry.api){t.Fatal(entry.signature,role,allowed)}
  }
 }
}
