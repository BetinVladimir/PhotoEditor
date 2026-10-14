package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)
func TestIntegrationExportReadCatalogPrivilegesAndAbsentScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01360 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected read catalog privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 var bucket string
 err=tx.QueryRow(ctx,`SELECT bucket FROM photo.v30_interchange_export_document($1,$2,$3,$4,$5,$6,$7,$8)`,tid,uuid.New(),uuid.New(),"missing-ledger",rawSHA([]byte("request")),uuid.New(),rawSHA([]byte("body")),"WorkingImage").Scan(&bucket)
 if err!=pgx.ErrNoRows{t.Fatal("absent scope returned metadata",err)}
}
func TestExportReadCatalogRequiresDatabaseBeforeLookup(t *testing.T){
 if _,err:=(SQLExportDocumentCatalog{}).LookupExportDocument(context.Background(),v2input.Entry{});err==nil{t.Fatal("missing DB accepted")}
}
