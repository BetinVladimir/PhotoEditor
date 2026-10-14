package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Deferred: isolated database with migration01356 is required. Negative authority
// and role tests do not constitute positive end-to-end storage/admission evidence.
func TestIntegrationExportDocumentScopeIsInternalAndFailsClosed(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal("database configuration failed")};defer db.Close()
 signature:="photo.v30_interchange_export_document_scope(uuid,uuid,uuid,text,text,uuid,text,text)"
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed{t.Fatalf("internal export scope executable by %s",role)}
 }
 var scope []byte
 if err:=db.QueryRow(ctx,`SELECT photo.v30_interchange_export_document_scope($1,$2,$3,$4,$5,$6,$7,$8)`,uuid.New(),uuid.New(),uuid.New(),"missing-key",rawSHA([]byte("request")),uuid.New(),"task_params","{}").Scan(&scope);err!=nil{t.Fatal(err)}
 if len(scope)!=0{t.Fatal("missing ledger/tenant setting granted document authority")}
}
