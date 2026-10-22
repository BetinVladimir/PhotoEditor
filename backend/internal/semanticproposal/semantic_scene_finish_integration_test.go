package semanticproposal

import (
 "context"
 "os"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Missing and cross-tenant children must fail closed without manufacturing a
// reservation, terminal run or completion event. Positive settlement/replay
// requires the complete producer fixture; this scenario does not replace it.
func TestIntegrationSceneFinishRejectsMissingAndForeignScope(t *testing.T) {
 url:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if url=="" {t.Skip("installed owned test database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 config,err:=pgxpool.ParseConfig(url);if err!=nil {t.Fatal(err)}
 config.ConnConfig.RuntimeParams["role"]="photo_fn_owner"
 db,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil {t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil {t.Fatal(err)};defer tx.Rollback(ctx)
 tenant,run,task,result:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 for _,scope:=range []string{"",uuid.NewString(),tenant.String()} {
  if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",scope);err!=nil {t.Fatal(err)}
  var accepted bool
  if err=tx.QueryRow(ctx,"SELECT photo.finish_semantic_scene_preparation($1,$2,$3,$4,$5,$6)",tenant,run,task,result,1,int64(1)).Scan(&accepted);err!=nil {t.Fatal(err)}
  if accepted {t.Fatalf("missing child accepted for scope %q",scope)}
  var resultReady bool
  if err=tx.QueryRow(ctx,"SELECT photo.semantic_scene_result_ready($1,$2,$3,$4,$5)",tenant,task,1,int64(1),`[{"artifact_id":"11111111-1111-4111-8111-111111111111","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"artifact_id":"22222222-2222-4222-8222-222222222222","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]`).Scan(&resultReady);err!=nil {t.Fatal(err)}
  if resultReady {t.Fatalf("missing producer result accepted for scope %q",scope)}
 }
 var count int64
 if err=tx.QueryRow(ctx,`SELECT
 (SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2)+
 (SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2)+
 (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2)+
 (SELECT count(*) FROM photo.tenant_event_cursors WHERE tenant_id=$1)`,tenant,run).Scan(&count);err!=nil {t.Fatal(err)}
 if count!=0 {t.Fatalf("rejected completion created durable state: %d rows",count)}
}
