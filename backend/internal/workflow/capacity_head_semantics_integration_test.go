package workflow

import (
 "context"
 "errors"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// Transaction-local eligible-node and residency doubles isolate the SQL
// selection rules from physical worker installation. Full node qualification
// and leader/commit behavior are separate acceptance scenarios.
func TestIntegrationCapacityHeadSnapshotAndAffinitySemantics(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01787 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 var actor,tenant,snapshot,run,params uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,"SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@capacity.example").Scan(&actor,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,"SELECT id FROM photo.tenants WHERE owner_user_id=$1",actor).Scan(&tenant);err!=nil{t.Fatal(err)}
 hash:=strings.Repeat("a",64)
 if err=tx.QueryRow(ctx,"INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id",tenant,uuid.NewString(),hash).Scan(&params);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,"INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,0) RETURNING id",tenant,actor,hash).Scan(&snapshot);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES($1,'v2','running',$2,'{"queue_class":"interactive"}'::jsonb,$3,$4,'fail_fast',true) RETURNING id`,tenant,snapshot,hash,actor).Scan(&run);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`CREATE OR REPLACE FUNCTION photo.eligible_dispatch_nodes(p_task uuid) RETURNS TABLE(node_id text)
 LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ SELECT 'fixture-node'::text $$`);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`CREATE OR REPLACE FUNCTION photo.capacity_task_affinity(p_tasks uuid[])
 RETURNS TABLE(task_id uuid,model_lock_sha text,resident boolean)
 LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT x.id,repeat('a',64),true FROM unnest(p_tasks) AS x(id)
 WHERE x.id=current_setting('photo.test_resident_task')::uuid $$`);err!=nil{t.Fatal(err)}
 // The cycle clock deliberately predates wall time. A task admitted after
 // p_now must not enter either the head or scoped affinity snapshot.
 now:=time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
 head,boundary,outside:=uuid.New(),uuid.New(),uuid.New()
 insert:=func(id uuid.UUID,class,state string,created time.Time){t.Helper()
  profile:="{\"backend\":\"cpu\"}";if class=="gpu"{profile="{\"backend\":\"gpu\"}"}
  _,e:=tx.Exec(ctx,`INSERT INTO photo.tasks(id,created_at,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES($1,$2,$3,$4,$5,'asset.verify','1',$6,$7,'[]'::jsonb,$8,$9,$10::jsonb)`,id,created,tenant,run,id.String(),class,state,params,hash,profile)
  if e!=nil{t.Fatal(e)}
 }
 insert(head,"cpu","ready",now.Add(-time.Minute))
 delayed:=uuid.New();insert(delayed,"cpu","ready",now.Add(-2*time.Minute))
 if _,err=tx.Exec(ctx,"UPDATE photo.tasks SET admission_after=$1 WHERE id=$2",now.Add(30*time.Second),delayed);err!=nil{t.Fatal(err)}
 delayedResident:=uuid.New();insert(delayedResident,"cpu","ready",now.Add(-35*time.Second))
 if _,err=tx.Exec(ctx,"UPDATE photo.tasks SET admission_after=$1 WHERE id=$2",now.Add(30*time.Second),delayedResident);err!=nil{t.Fatal(err)}
 insert(boundary,"cpu","ready",now.Add(-30*time.Second))
 insert(outside,"cpu","ready",now.Add(-29*time.Second))
 gpu:=uuid.New();insert(gpu,"gpu","ready",now.Add(-time.Minute))
 insert(uuid.New(),"gpu","queued",now.Add(-time.Minute))
 insert(uuid.New(),"gpu","queued",now.Add(-time.Minute))
 // A second tenant contributes another interactive FIFO head and a capture
 // head. The SQL choice must apply both tenant wrap and class cursor ordering.
 var otherActor,otherTenant,otherSnapshot,otherParams,otherRun,otherCaptureRun uuid.UUID
 if err=tx.QueryRow(ctx,"SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@capacity.example").Scan(&otherActor);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,"SELECT id FROM photo.tenants WHERE owner_user_id=$1",otherActor).Scan(&otherTenant);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,"INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id",otherTenant,uuid.NewString(),hash).Scan(&otherParams);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,"INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,0) RETURNING id",otherTenant,otherActor,hash).Scan(&otherSnapshot);err!=nil{t.Fatal(err)}
 for _,entry:=range []struct{class string;dest *uuid.UUID}{{"interactive",&otherRun},{"capture",&otherCaptureRun}}{
  if err=tx.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES($1,'v2','running',$2,jsonb_build_object('queue_class',$3::text),$4,$5,'fail_fast',true) RETURNING id`,otherTenant,otherSnapshot,entry.class,hash,otherActor).Scan(entry.dest);err!=nil{t.Fatal(err)}
  taskID:=uuid.New()
  if _,err=tx.Exec(ctx,`INSERT INTO photo.tasks(id,created_at,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES($1,$2,$3,$4,$5,'asset.verify','1','cpu','ready','[]'::jsonb,$6,$7,'{"backend":"cpu"}'::jsonb)`,taskID,now.Add(-time.Minute),otherTenant,*entry.dest,taskID.String(),otherParams,hash);err!=nil{t.Fatal(err)}
 }
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:1800,TenantHeavyQueued:2}
 heads,err:=ReadCapacityHeadSnapshot(ctx,tx,"cpu",now,policy);if err!=nil{t.Fatal(err)}
 selector,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 for _,class:=range capacityClasses{selector.deficits[class]=1800}
 expected,err:=selector.nextHead(heads.Heads,now);if err!=nil{t.Fatal(err)}
 bounded,err:=ReadCapacityBoundedHeadChoice(ctx,tx,"cpu",now,policy,uuid.Nil,0,selector.deficits,nil)
 if err!=nil||expected==nil||bounded.Head==nil||bounded.Head.TaskID!=expected.TaskID||bounded.EffectiveClass!=expected.class||bounded.MaxRunnableCostSeconds!=heads.MaxRunnableCostSeconds{t.Fatal("bounded SQL choice differs from full FIFO snapshot",expected,bounded,err)}
 excluded,err:=ReadCapacityBoundedHeadChoice(ctx,tx,"cpu",now,policy,uuid.Nil,0,selector.deficits,[]uuid.UUID{expected.TaskID})
 if err!=nil||excluded.Head!=nil&&excluded.Head.TaskID==expected.TaskID{t.Fatal("bounded SQL exclusion ignored",excluded,err)}
 for cursor:=0;cursor<len(capacityClasses);cursor++{for _,last:=range []uuid.UUID{uuid.Nil,tenant,otherTenant}{
  current,createErr:=NewCapacityDRR(policy);if createErr!=nil{t.Fatal(createErr)}
  current.classCursor=cursor;current.lastTenant=last
  for _,class:=range capacityClasses{current.deficits[class]=1800}
  want,selectErr:=current.nextHead(heads.Heads,now);if selectErr!=nil{t.Fatal(selectErr)}
  got,readErr:=ReadCapacityBoundedHeadChoice(ctx,tx,"cpu",now,policy,last,cursor,current.deficits,nil)
  if readErr!=nil||want==nil||got.Head==nil||got.Head.TaskID!=want.TaskID||got.EffectiveClass!=want.class{
   t.Fatal("bounded SQL DRR order changed",cursor,last,want,got,readErr)
  }
 }}
 found:=false
 for _,candidate:=range heads.Heads{if candidate.TenantID==tenant{if candidate.TaskID!=head{t.Fatal("wrong tenant FIFO head",candidate)};found=true}}
 if !found{t.Fatal("tenant omitted from head snapshot")}
 gpuHeads,err:=ReadCapacityHeadSnapshot(ctx,tx,"gpu",now,policy);if err!=nil{t.Fatal(err)}
 for _,candidate:=range gpuHeads.Heads{if candidate.TenantID==tenant{t.Fatal("heavy-at-cap task admitted",candidate)}}
 selection:=&CapacitySelection{TaskID:head,tenant:tenant,class:"interactive",cost:1800,createdAt:now.Add(-time.Minute)}
 staleHead:=&CapacitySelection{TaskID:boundary,tenant:tenant,class:"interactive",cost:1800,createdAt:now.Add(-30*time.Second)}
 if _,err=ReadCapacityHeadAffinity(ctx,tx,"cpu",staleHead,now,policy,nil);!errors.Is(err,pgx.ErrNoRows){t.Fatal("non-FIFO head accepted",err)}
 for _,tc:=range []struct{resident,want uuid.UUID}{{boundary,boundary},{outside,head}}{
  if _,err=tx.Exec(ctx,"SELECT set_config('photo.test_resident_task',$1,true)",tc.resident.String());err!=nil{t.Fatal(err)}
  hint,e:=ReadCapacityHeadAffinity(ctx,tx,"cpu",selection,now,policy,nil)
  if e!=nil||hint.Candidate.TaskID!=tc.want{t.Fatal("wrong thirty-second affinity",hint,e,tc.want)}
 }
 if _,err=tx.Exec(ctx,"SELECT set_config('photo.test_resident_task',$1,true)",delayedResident.String());err!=nil{t.Fatal(err)}
 if hint,e:=ReadCapacityHeadAffinity(ctx,tx,"cpu",selection,now,policy,nil);e!=nil||hint.Candidate.TaskID!=head{t.Fatal("post-clock admitted task entered scoped affinity",hint,e)}
 if _,err=ReadCapacityHeadAffinity(ctx,tx,"gpu",selection,now,policy,nil);!errors.Is(err,pgx.ErrNoRows){t.Fatal("cross-pool head accepted",err)}
 // A previously valid head must become stale when an earlier eligible task
 // arrives after the snapshot, before this transaction chooses affinity.
 // Queue mutation is fixture setup under the original isolated DB role.
 // Scheduler reads remain under its production EXECUTE-only authority.
 if _,err=tx.Exec(ctx,"RESET ROLE");err!=nil{t.Fatal(err)}
 newHead:=uuid.New();insert(newHead,"cpu","ready",now.Add(-90*time.Second))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
 if _,err=ReadCapacityHeadAffinity(ctx,tx,"cpu",selection,now,policy,nil);!errors.Is(err,pgx.ErrNoRows){t.Fatal("stale FIFO head accepted after queue change",err)}
 // A 5,000-task burst must still yield the same bounded DRR choice and
 // tenant FIFO head without returning those tasks to the application.
 if _,err=tx.Exec(ctx,"RESET ROLE");err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`WITH ids AS (
  SELECT gen_random_uuid() AS task_id FROM generate_series(1,5000)
 ) INSERT INTO photo.tasks(id,created_at,tenant_id,run_id,node_key,operation,operation_version,
   resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 SELECT task_id,$1,$2,$3,task_id::text,'asset.verify','1','cpu','ready',
   '[]'::jsonb,$4,$5,'{"backend":"cpu"}'::jsonb FROM ids`,
  now.Add(-10*time.Second),tenant,run,params,hash);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
 largeHeads,err:=ReadCapacityHeadSnapshot(ctx,tx,"cpu",now,policy);if err!=nil{t.Fatal(err)}
 largeSelector,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 for _,class:=range capacityClasses{largeSelector.deficits[class]=1800}
 want,err:=largeSelector.nextHead(largeHeads.Heads,now);if err!=nil||want==nil{t.Fatal("large queue head unavailable",err)}
 got,err:=ReadCapacityBoundedHeadChoice(ctx,tx,"cpu",now,policy,uuid.Nil,0,largeSelector.deficits,nil)
 if err!=nil||got.Head==nil||got.Head.TaskID!=want.TaskID||got.EffectiveClass!=want.class{
  t.Fatal("bounded choice dropped or reordered a tenant at 5000 tasks",got,want,err)
 }
 currentTenantFound:=false
 for _,candidate:=range largeHeads.Heads{if candidate.TenantID==tenant{
  if candidate.TaskID!=newHead{t.Fatal("large burst displaced older tenant FIFO head",candidate)}
  currentTenantFound=true
 }}
 if !currentTenantFound{t.Fatal("large burst omitted tenant head")}
}
