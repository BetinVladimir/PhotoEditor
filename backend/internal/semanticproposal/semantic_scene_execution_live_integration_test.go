package semanticproposal

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Supply an actual sealed/registered, independently qualified scene child.
// This scenario does not synthesize qualification or replace physical inference.
func TestIntegrationSceneExecutionLiveWithdrawalAndReadOnly(t *testing.T) {
 raw:=os.Getenv("PHOTO_SCENE_EXECUTION_LIVE_CASE")
 if raw=="" {t.Skip("actual qualified sealed scene/depth fixture required")}
 var f struct{Tenant,Run,Task uuid.UUID}
 if json.Unmarshal([]byte(raw),&f)!=nil||f.Tenant==uuid.Nil||f.Run==uuid.Nil||f.Task==uuid.Nil {t.Fatal("invalid fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 cfg,err:=pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"));if err!=nil {t.Fatal(err)}
 db,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil {t.Fatal(err)};defer db.Close()
 open:=func(readonly bool) pgx.Tx {t.Helper();options:=pgx.TxOptions{};if readonly {options.AccessMode=pgx.ReadOnly}
  tx,e:=db.BeginTx(ctx,options);if e!=nil {t.Fatal(e)}
  if _,e=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");e!=nil {t.Fatal(e)}
  if _,e=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",f.Tenant.String());e!=nil {t.Fatal(e)};return tx}
 check:=func(tx pgx.Tx,want bool){t.Helper();var got bool
  if e:=tx.QueryRow(ctx,"SELECT photo.execution_source_live($1)",f.Task).Scan(&got);e!=nil||got!=want {t.Fatalf("live=%v want=%v err=%v",got,want,e)}}
 // PostgreSQL read-only mode refuses FOR UPDATE/SHARE and writes, including
 // nested helpers; a passing source predicate must not invert canonical locks.
 ro:=open(true);check(ro,true);_ = ro.Rollback(ctx)
 mutations:=[]string{
  `UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=$1 AND id=(SELECT (config_snapshot->'v27_scene_prepare'->>'parent_run_id')::uuid FROM photo.runs WHERE tenant_id=$1 AND id=$2)`,
  `UPDATE photo.operation_registry SET v2_enabled=false WHERE (operation,version)=(SELECT operation,operation_version FROM photo.tasks WHERE tenant_id=$1 AND id=$3)`,
  `UPDATE photo.v2_qualification_policies SET enabled=false WHERE operation=(SELECT operation FROM photo.tasks WHERE tenant_id=$1 AND id=$3) AND backend='cuda'`,
  `UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=(SELECT (config_snapshot->'v27_scene_prepare'->'source'->'source'->>'artifact_id')::uuid FROM photo.runs WHERE tenant_id=$1 AND id=$2)`,
  `UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=$1 AND id=$3)`,
  `DELETE FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$3 AND artifact_id=(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=$1 AND id=$3)`,
 }
 for _,mutation:=range mutations {tx:=open(false);check(tx,true)
  if _,e:=tx.Exec(ctx,"RESET ROLE");e!=nil {t.Fatal(e)}
  tag,e:=tx.Exec(ctx,"WITH fixture AS (SELECT $1::uuid tenant,$2::uuid run,$3::uuid task) "+mutation,f.Tenant,f.Run,f.Task);if e!=nil {t.Fatal(e)};if tag.RowsAffected()==0 {t.Fatal("fixture mutation did not exercise authority")}
  if _,e=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");e!=nil {t.Fatal(e)}
  check(tx,false);_ = tx.Rollback(ctx)
 }
 tx:=open(true);check(tx,true);_ = tx.Rollback(ctx)
}

// Catalog evidence is installation-only. The full fixture above proves actual
// predicate behavior; neither scenario qualifies a physical model or GPU.
func TestIntegrationSceneExecutionStartAndTriggerInstalled(t *testing.T) {
 url:=os.Getenv("PHOTO_TEST_DATABASE_URL");if url=="" {t.Skip("owned installed database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,url);if err!=nil {t.Fatal(err)};defer db.Close()
 var ready bool
 err=db.QueryRow(ctx,`SELECT
 (SELECT count(*)=1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND position('stage_name:=CASE task_row.operation' in p.prosrc)>0
 AND position('WHEN ''scene.parse'' THEN ''v27_scene_prepare''' in p.prosrc)>0
 AND position('WHEN ''depth.predict'' THEN ''v27_scene_prepare''' in p.prosrc)>0
 AND position('UPDATE photo.runs SET status=''running''' in p.prosrc)>0
 AND position('''face.parse'',''scene.parse'',''depth.predict'')' in p.prosrc)>0)
 AND EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='photo.guard_v2_result_root()'::regprocedure
 AND position('photo.semantic_scene_result_ready' in p.prosrc)>0
 AND position('photo.semantic_scene_result_ready' in p.prosrc)<position('photo.semantic_target_defect_result_ready' in p.prosrc))
 AND EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='photo.execution_source_live(uuid)'::regprocedure
 AND position('AND photo.semantic_scene_task_live(p_task) IS TRUE' in p.prosrc)>0)`).Scan(&ready)
 if err!=nil||!ready {t.Fatalf("scene execution installation missing: %v",err)}
}
