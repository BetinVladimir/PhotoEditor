package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationExportDraftAdmissionPrivilegesAndAbsentScope(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL"); if dsn=="" {t.Skip("isolated DB migration01372 required")}
 ctx:=context.Background(); db,err:=pgxpool.New(ctx,dsn); if err!=nil {t.Fatal(err)}; defer db.Close()
 for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal"} {
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_interchange_export_draft_ready(uuid,uuid,uuid,text,text,text)','EXECUTE')`,role).Scan(&allowed);err!=nil {t.Fatal(err)}
  if allowed!=(role=="photo_api") {t.Fatal("draft admission privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil {t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil {t.Fatal(err)}
 var ready bool
 if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_export_draft_ready($1,$2,$3,$4,$5,$6)`,tid,uuid.New(),uuid.New(),"absent-key","aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb").Scan(&ready);err!=nil||ready {t.Fatal("absent draft admitted",err)}
}
