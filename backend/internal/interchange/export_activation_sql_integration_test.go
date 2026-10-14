package interchange
import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportActivationPrivilegesAndInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01374 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.activate_v30_interchange_export(uuid,uuid,uuid,text,text,jsonb)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("activation privilege",role)}
 }
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.activate_v30_interchange_export(uuid,uuid,uuid,text,text,jsonb)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 for _,required:=range []string{"v30_interchange_export_draft_ready","seal_v2_graph_pre_v30_export","INTERCHANGE_EXPORT_ACTIVATION_REFUSED","Interchange.Export.Ticket","p_task IS DISTINCT FROM expected"}{if !strings.Contains(definition,required){t.Fatal("missing activation guard",required)}}
}
