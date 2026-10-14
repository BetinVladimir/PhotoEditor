package v2optical

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Refusal and installed-wiring evidence only. A successful settled closure and
// whole-run completion still require the isolated lifecycle qualification.
func TestIntegrationNegativeSkipReviewRefusesMissingAuthority(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
 for _,scope:=range []struct{tenant,task any}{
  {nil,nil},{uuid.New(),nil},{nil,uuid.New()},{uuid.New(),uuid.New()},
 }{
  var omitted bool
  if err=tx.QueryRow(ctx,"SELECT photo.v29_negative_skip_task_omitted($1,$2)",scope.tenant,scope.task).Scan(&omitted);err!=nil{t.Fatal(err)}
  if omitted{t.Fatal("missing tenant/task/closure authority omitted a task")}
 }
 for _,fn:=range []string{"photo.finalize_v2_run(uuid,uuid)","photo.v2_pending_finalizations()"}{
  var definition string
  if err=tx.QueryRow(ctx,"SELECT pg_get_functiondef($1::regprocedure)",fn).Scan(&definition);err!=nil{t.Fatal(err)}
  taskGate,reviewGate:="photo.v29_negative_skip_task_omitted(task.tenant_id,task.id)","photo.v29_negative_skip_task_omitted(v.tenant_id,v.task_id)"
  if strings.Contains(fn,"pending"){
   taskGate,reviewGate="photo.v29_negative_skip_task_omitted(rt.tenant_id,rt.id)","photo.v29_negative_skip_task_omitted(rv.tenant_id,rv.task_id)"
  }
  if !strings.Contains(definition,taskGate)||!strings.Contains(definition,reviewGate){t.Fatal("missing installed task/review omission gate",fn)}
 }
}

// Structural regression guard only; does not certify positive run lifecycle.
func TestIntegrationNegativeSkipLeafKeepsCancellationAndFailure(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var helper,leaf,pending string
 if err=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.v29_negative_skip_task_omitted(uuid,uuid)'::regprocedure),pg_get_functiondef('photo.finalize_v2_run_pre_semantic(uuid,uuid)'::regprocedure),pg_get_functiondef('photo.v2_pending_finalizations()'::regprocedure)`).Scan(&helper,&leaf,&pending);err!=nil{t.Fatal(err)}
 if !strings.Contains(helper,"dependent.state IN('succeeded','cancelled','skipped')")||strings.Contains(helper,"dependent.state IN('succeeded','failed'"){t.Fatal("real task failure may be omitted")}
 for _,definition:=range []string{leaf,pending}{
  if !strings.Contains(definition,"r.cancel_requested OR r.status='cancelling'"){t.Fatal("run cancellation gate lost")}
  if !strings.Contains(definition,"AND NOT photo.v29_negative_skip_task_omitted(t.tenant_id,t.id) AND t.state IN ('failed','cancelled','skipped')"){t.Fatal("failure gate lost")}
  if !strings.Contains(definition,"AND NOT photo.v29_negative_skip_task_omitted(t.tenant_id,t.id) AND (t.state<>'succeeded'"){t.Fatal("adoption gate lost")}
 }
 if !strings.Contains(leaf,"AND NOT photo.v29_negative_skip_task_omitted(t.tenant_id,t.id) AND z.review_required"){t.Fatal("required review gate lost")}
}

func TestIntegrationAdoptedOpticalParentRefusesMissingScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
 tenant:=uuid.New()
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{t.Fatal(err)}
 for _,who:=range []struct{actor,session any}{{nil,nil},{uuid.New(),nil},{nil,uuid.New()},{uuid.New(),uuid.New()}}{
  var refused bool
  if err=tx.QueryRow(ctx,"SELECT photo.read_v29_adopted_optical_parent($1,$2,$3,$4,$5,$6) IS NULL",tenant,uuid.New(),who.actor,who.session,uuid.New(),uuid.New()).Scan(&refused);err!=nil{t.Fatal(err)}
  if !refused{t.Fatal("missing live scope returned parent")}
 }
}

func TestIntegrationAdoptedParentArrayRefusesUnadoptedSource(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
 tenant:=uuid.New();if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{t.Fatal(err)}
 var missingInputScope bool
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_affected_source_inputs($1,$2,$3,$4,$5,$6,$7) IS NULL",tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),strings.Repeat("a",64),uuid.New()).Scan(&missingInputScope);err!=nil{t.Fatal(err)}
 if !missingInputScope{t.Fatal("unknown review/consumer exposed source mapping")}
 var missingUnchangedScope bool
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_unchanged_source($1,$2,$3,$4,$5,$6,$7,$8,$9) IS NULL",tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),strings.Repeat("a",64),uuid.New(),uuid.New(),strings.Repeat("b",64)).Scan(&missingUnchangedScope);err!=nil{t.Fatal(err)}
 if !missingUnchangedScope{t.Fatal("unscoped unchanged source exposed")}
 var unchangedLockAllowed bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v29_negative_unchanged_proof($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12::jsonb,$13::jsonb)",tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),strings.Repeat("a",64),"{}",uuid.New(),uuid.New(),strings.Repeat("b",64),"{}","{}","[]").Scan(&unchangedLockAllowed);err!=nil{t.Fatal(err)}
 if unchangedLockAllowed{t.Fatal("empty unchanged proof accepted")}
 var unknownMember bool
 if err=tx.QueryRow(ctx,"SELECT photo.v29_negative_replacement_member($1,$2,$3,$4,$5,$6,$7,$8,$9)",tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),strings.Repeat("a",64),uuid.New(),uuid.New(),uuid.New()).Scan(&unknownMember);err!=nil{t.Fatal(err)}
 if unknownMember{t.Fatal("unknown replacement membership accepted")}



 for _,role:=range []string{"WorkingPixels","WorkingAlpha","InverseField/0","InverseField/128","other"}{
  var refused bool
  if err=tx.QueryRow(ctx,"SELECT photo.read_v29_adopted_optical_parent_array($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) IS NULL",tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),"{}",uuid.New(),strings.Repeat("a",64),role).Scan(&refused);err!=nil{t.Fatal(err)}
  if !refused{t.Fatal("array source without live adopted parent accepted")}
  var unchangedRefused bool
  if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_unchanged_array($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) IS NULL",tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),strings.Repeat("a",64),uuid.New(),uuid.New(),strings.Repeat("b",64),"{}",uuid.New(),strings.Repeat("c",64),role).Scan(&unchangedRefused);err!=nil{t.Fatal(err)}
  if !unchangedRefused{t.Fatal("unscoped unchanged array exposed")}

 }
}
