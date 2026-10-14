package interchange
import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportSealGuardPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01373 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_portal"}{
  var allowed,predecessor bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.seal_v2_graph(uuid,uuid)','EXECUTE'),has_function_privilege($1,'photo.seal_v2_graph_pre_v30_export(uuid,uuid)','EXECUTE')`,role).Scan(&allowed,&predecessor);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler")||predecessor{t.Fatal("seal bypass privilege",role)}
 }
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.seal_v2_graph(uuid,uuid)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 for _,required:=range []string{"v2.interchange.export","RETURN false","seal_v2_graph_pre_v30_export"}{if !strings.Contains(definition,required){t.Fatal("missing seal guard",required)}}
}
