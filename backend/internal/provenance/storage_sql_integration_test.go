package provenance

import (
 "context"
 "os"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationProvenanceStoragePrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations 01387/01388/01389 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var enabled,forced bool
 if err:=db.QueryRow(ctx,`SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.provenance_manifests'::regclass`).Scan(&enabled,&forced);err!=nil||!enabled||!forced{t.Fatal("tenant isolation missing",err)}
 for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_auth","photo_portal","photo_publisher","photo_maintenance","photo_gateway","photo_fn_owner"}{
  for _,privilege:=range []string{"SELECT","INSERT","UPDATE","DELETE","TRUNCATE"}{
   var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_table_privilege($1,'photo.provenance_manifests',$2)`,role,privilege).Scan(&allowed);err!=nil{t.Fatal(err)}
   expected:=role=="photo_fn_owner"&&(privilege=="SELECT"||privilege=="INSERT"||privilege=="DELETE")
   if allowed!=expected{t.Fatal("unexpected provenance storage privilege",role,privilege,allowed)}
  }
 }
 for _,trigger:=range []string{"ready_artifact_reference","ready_provenance_export"}{
  var installed bool
  if err:=db.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='photo.provenance_manifests'::regclass AND tgname=$1 AND NOT tgisinternal AND tgenabled='O')`,trigger).Scan(&installed);err!=nil||!installed{t.Fatal("reference guard missing",trigger,err)}
 }
 var immutable bool
 if err:=db.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='photo.provenance_manifests'::regclass AND tgname='immutable_content' AND NOT tgisinternal AND tgenabled='O')`).Scan(&immutable);err!=nil||!immutable{t.Fatal("immutable storage trigger missing",err)}
}
