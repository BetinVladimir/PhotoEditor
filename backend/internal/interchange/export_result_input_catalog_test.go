package interchange
import (
 "context"
 "os"
 "testing"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)
func TestExportResultInputCatalogueAndResolverRequireAuthority(t *testing.T){
 catalog:=SQLExportResultInputCatalog{}
 if _,err:=catalog.LookupExportDocument(context.Background(),v2input.Entry{});err==nil{t.Fatal("missing catalogue authority")}
 if _,err:=NewSQLExportResultResolver(context.Background(),catalog,nil,nil,nil,1<<20);err==nil{t.Fatal("missing resolver installation")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=catalog.LookupExportDocument(ctx,v2input.Entry{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if _,err:=NewSQLExportResultResolver(ctx,catalog,nil,nil,nil,1<<20);err!=context.Canceled{t.Fatal("resolver cancellation",err)}
}
func TestIntegrationExportResultInputCataloguePrivilegesAndAbsentAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01363 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected result input privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 for _,contract:=range []string{"WorkingImage","FaceObservations","EditingRecipeV3","EncodedImage","StyleBackgroundAsset","WorkingPixels","WorkingAlpha","DenseWarpField","MaskPixels","foreign"}{
  var bucket string
  err=tx.QueryRow(ctx,`SELECT bucket FROM photo.v30_interchange_export_result_input($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,tid,uuid.New(),uuid.New(),"missing-ledger",rawSHA([]byte("request")),uuid.New(),rawSHA([]byte("root")),uuid.New(),rawSHA([]byte("body")),contract).Scan(&bucket)
  if err!=pgx.ErrNoRows{t.Fatal("absent authority returned input metadata",contract,err)}
 }
}

// This checks the installed migration predicate, not a successful export lifecycle.
func TestIntegrationExportResultInputPreservesNativeMedia(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated database required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 for _,clause:=range []string{
  "WHEN p_contract IN('EncodedImage','StyleBackgroundAsset') THEN a.media_type IN('image/jpeg','image/png','image/tiff')",
  "THEN a.media_type IN('application/octet-stream','application/x-npy')",
  "ELSE a.media_type='application/json' END",
  "photo.v30_interchange_export_output", "i.contract=p_contract", "a.object_key",
 }{if !strings.Contains(definition,clause){t.Fatal("result input media/scope predicate missing",clause)}}
}
