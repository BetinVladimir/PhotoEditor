package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)
func TestIntegrationExportMaterializationPrivilegesAndAbsentAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01359 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.materialize_v30_interchange_export(uuid,uuid,uuid,text,text,text,jsonb)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected materializer privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid,actor:=uuid.New(),uuid.New();key:="absent-materialization";run:=uuid.NewSHA1(tid,[]byte("interchange:create:"+actor.String()+":"+key))
 if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 var accepted bool
 if err:=tx.QueryRow(ctx,`SELECT photo.materialize_v30_interchange_export($1,$2,$3,$4,$5,'{}','{}'::jsonb)`,tid,actor,uuid.New(),key,rawSHA([]byte("request"))).Scan(&accepted);err!=nil{t.Fatal(err)}
 if accepted{t.Fatal("absent authority materialized")}
 var count int
 if err:=tx.QueryRow(ctx,`SELECT (SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2)+(SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2)`,tid,run).Scan(&count);err!=nil{t.Fatal(err)}
 if count!=0{t.Fatal("denied materialization left task/quota")}
}
func TestExportMaterializerRequiresDatabase(t *testing.T){
 if err:=(SQLExportTaskMaterializer{}).MaterializeExport(context.Background(),CreateIntent{},FrozenExportDocuments{},v2input.DraftTask{});err==nil{t.Fatal("missing database accepted")}
}
