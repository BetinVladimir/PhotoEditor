package v2optical

import (
 "context"
 "os"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// These are rejection/ACL checks only; a positive real-object lifecycle fixture
// is still required before this migration or producer path is qualified.
func TestIntegrationNegativeDraftSaveIsSchedulerOnlyAndRejectsInvalidScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_auth","photo_publisher","photo_maintenance","photo_portal"}{
  var permitted bool
  if err:=db.QueryRow(ctx,"SELECT has_function_privilege($1,'photo.save_v29_negative_draft(uuid,uuid,uuid,text)','EXECUTE')",role).Scan(&permitted);err!=nil{t.Fatal(err)}
  if permitted!=(role=="photo_scheduler"){t.Fatal("wrong function privilege",role,permitted)}
 }
 for _,raw:=range []string{"{}","null","[]","{bad"}{
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)}
  tenant:=uuid.New()
  if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{tx.Rollback(ctx);t.Fatal(err)}
  var saved bool
  err=tx.QueryRow(ctx,"SELECT photo.save_v29_negative_draft($1,$2,$3,$4)",tenant,uuid.New(),uuid.New(),raw).Scan(&saved)
  tx.Rollback(ctx)
  if err!=nil||saved{t.Fatal("invalid scope not safely refused",raw,err)}
 }
}
