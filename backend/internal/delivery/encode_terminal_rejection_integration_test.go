package delivery

import (
 "context"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestDeliveryEncodeTerminalRejectionRequiresSameResultAndSevenRefusals(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn=="" {t.Skip("isolated migrated PostgreSQL required")}
 ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil {t.Fatal(err)}
 defer db.Close()
 signature:="photo.reject_v30_delivery_encode_after_retries(uuid,uuid,text)"
 var definition string
 var scheduler,api,worker bool
 err=db.QueryRow(ctx,`SELECT pg_get_functiondef($1::regprocedure),
  has_function_privilege('photo_scheduler',$1,'EXECUTE'),
  has_function_privilege('photo_api',$1,'EXECUTE'),
  has_function_privilege('photo_worker',$1,'EXECUTE')`,signature).
  Scan(&definition,&scheduler,&api,&worker)
 if err!=nil||!scheduler||api||worker {t.Fatal("terminal rejection ACL drift",err)}
 for _,gate:=range []string{"v30_encode_retry_result", "canonical_result_id", "v30_encode_retry_count", "v30_delivery_encode_context", "reject_v2_adoption"} {
  if !strings.Contains(definition,gate) {t.Fatalf("terminal rejection lacks %s",gate)}
 }
 tx,err:=db.Begin(ctx);if err!=nil {t.Fatal(err)}
 defer tx.Rollback(ctx)
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil {t.Fatal(err)}
 var rejected bool
 if err=tx.QueryRow(ctx,"SELECT photo.reject_v30_delivery_encode_after_retries($1,$2,$3)",
  uuid.New(),uuid.New(),"DELIVERY_ENCODE_PHYSICAL_INVALID").Scan(&rejected);err!=nil {t.Fatal(err)}
 if rejected {t.Fatal("absent tenant/task terminally rejected")}
}
