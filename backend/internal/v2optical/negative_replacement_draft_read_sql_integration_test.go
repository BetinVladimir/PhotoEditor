package v2optical

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// ACL/refusal coverage only. A positive original-context lifecycle is required
// separately; these cases do not qualify persistence or replacement admission.
func TestIntegrationReplacementDraftReadSchedulerScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.read_v29_negative_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid)"
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_auth","photo_publisher","photo_maintenance","photo_portal"}{
  var permitted bool
  if err:=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,signature).Scan(&permitted);err!=nil{t.Fatal(err)}
  if permitted!=(role=="photo_scheduler"){t.Fatal("wrong function privilege",role,permitted)}
 }
 for _,snapshot:=range []any{nil,uuid.Nil,uuid.New()}{
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)}
  tenant:=uuid.New()
  if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{tx.Rollback(ctx);t.Fatal(err)}
  var saved *string
  err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_replacement_draft($1,$2,$3,$4,$5,$6,$7,$8)",tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",uuid.New(),snapshot).Scan(&saved)
  tx.Rollback(ctx)
  if err!=nil||saved!=nil{t.Fatal("unowned draft returned",snapshot,err)}
 }
}
