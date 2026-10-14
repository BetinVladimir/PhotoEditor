package interchange
import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportAdoptionGuardAndTenantPurgeInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations01365-01366 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_portal"}{
  var allowed,predecessor bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE'),has_function_privilege($1,'photo.adopt_v2_plan_pre_export_guard(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')`,role).Scan(&allowed,&predecessor);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler")||predecessor{t.Fatal("adoption bypass privilege",role)}
 }
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 for _,required:=range []string{"'v30_interchange_export_evidence'","'v30_interchange_ingest_outcomes'","cleanup_lease_until>clock_timestamp()","x.status='deleting'"}{if !strings.Contains(definition,required){t.Fatal("missing purge table/precondition",required)}}
 for _,table:=range []string{"photo.v30_interchange_export_evidence","photo.v30_interchange_ingest_outcomes"}{
  var ownerDelete,apiDelete bool
  if err:=db.QueryRow(ctx,`SELECT has_table_privilege('photo_fn_owner',$1,'DELETE'),has_table_privilege('photo_api',$1,'DELETE')`,table).Scan(&ownerDelete,&apiDelete);err!=nil{t.Fatal(err)}
  if !ownerDelete||apiDelete{t.Fatal("unexpected purge privilege",table)}
 }
}
