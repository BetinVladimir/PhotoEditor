package interchange
import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportCatalogRootAuthorityInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01375 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)"
 for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal"}{
  var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("root catalogue privilege",role)}
 }
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef($1::regprocedure)`,signature).Scan(&definition);err!=nil{t.Fatal(err)}
 for _,required:=range []string{"v30_interchange_export_document_scope","asset.original_artifact_id=a.id","v.master_artifact_id=a.id","v26_render_recipe_scope","stream.asset_version_id","d.document_type='EditingRecipeV3'","object_version NOT IN('','null')"}{if !strings.Contains(definition,required){t.Fatal("missing authority predicate",required)}}
}
