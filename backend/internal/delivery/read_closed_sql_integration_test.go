package delivery

import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationDeliveryReaderUsesClosedPublicProjection(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01777 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.get_v30_delivery(uuid,uuid,uuid,uuid)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 if strings.Contains(definition,"to_jsonb(d)")||!strings.Contains(definition,"'data',jsonb_build_object("){
  t.Fatal("delivery reader exposes table-row columns")
 }
 for _,field:=range []string{"'resource_type'","'id'","'tenant_id'","'project_id'","'created_at'","'revision'","'updated_at'","'workflow_id'","'snapshot_id'","'name'","'policy_artifact_id'","'manifest_artifact_id'","'published_at'","'state'"}{
  if !strings.Contains(definition,field){t.Fatal("delivery projection omitted schema field",field)}
 }
 for _,predicate:=range []string{"a.media_type='application/json'","a.output_role='document'","a.verified_at IS NOT NULL","a.object_version IS NOT NULL"}{
  if !strings.Contains(definition,predicate){t.Fatal("delivery document readiness predicate absent",predicate)}
 }
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.get_v30_delivery(uuid,uuid,uuid,uuid)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("delivery read privilege escaped API",role)}
 }
}
