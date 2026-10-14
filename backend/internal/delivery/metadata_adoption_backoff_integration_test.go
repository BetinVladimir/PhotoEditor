package delivery

import (
 "context"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestMetadataAdoptionSettlementInstalledWithScopedRetry(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,signature:=range []string{"photo.next_v30_metadata_adoption(uuid)","photo.settle_v30_metadata_invalid(uuid,uuid,integer,bigint,uuid,text)"}{
  var body string;var scheduler,api,worker bool
  err=db.QueryRow(ctx,`SELECT pg_get_functiondef($1::regprocedure),
   has_function_privilege('photo_scheduler',$1,'EXECUTE'),
   has_function_privilege('photo_api',$1,'EXECUTE'),
   has_function_privilege('photo_worker',$1,'EXECUTE')`,signature).Scan(&body,&scheduler,&api,&worker)
  if err!=nil||!scheduler||api||worker{t.Fatalf("metadata settlement ACL drift %s: %v",signature,err)}
  for _,gate:=range []string{"v30_metadata_retry_result","v30_metadata_retry_at","canonical_result_id"}{
   if !strings.Contains(body,gate){t.Fatalf("%s missing %s",signature,gate)}
  }
  if strings.Contains(signature,"settle_"){
   for _,gate:=range []string{"v30_metadata_result_ready","v30_metadata_retry_count","reject_v2_adoption"}{
    if !strings.Contains(body,gate){t.Fatalf("settlement missing %s",gate)}
   }
  }
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
 tenant,task,result:=uuid.New(),uuid.New(),uuid.New()
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{t.Fatal(err)}
 var state string
 err=tx.QueryRow(ctx,`SELECT photo.settle_v30_metadata_invalid($1,$2,1,1,$3,'DELIVERY_METADATA_RASTER_BYTES')`,
  tenant,task,result).Scan(&state)
 if err!=nil||state!="stale"{t.Fatal("absent metadata task was settled",state,err)}
}
