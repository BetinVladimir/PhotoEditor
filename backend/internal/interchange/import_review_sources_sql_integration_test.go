package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationImportReviewSourcesKeepPrivateAuthorityAndRejectAbsentWorkflow(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration 01378 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_interchange_import_sources(uuid,uuid,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected source authority privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tenant:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{t.Fatal(err)}
 var refused bool
 if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_import_sources($1,$2,'{}') IS NULL`,tenant,uuid.New()).Scan(&refused);err!=nil||!refused{t.Fatal("absent source accepted",err)}
 if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_import_sources($1,$2,'{}') IS NULL`,uuid.New(),uuid.New()).Scan(&refused);err!=nil||!refused{t.Fatal("foreign tenant accepted",err)}
}
