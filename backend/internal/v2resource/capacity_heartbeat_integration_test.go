package v2resource

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Requires an isolated admin DB migrated through01632; fixture always rolls back.
func TestIntegrationCapacityPolicyArchiveHeartbeat(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated DB required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 exec:=func(query string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,query,args...);err!=nil{t.Fatal(err)}}
 var user,tenant uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@capacity.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,user).Scan(&tenant);err!=nil{t.Fatal(err)}
 worker:=uuid.New();node:="capacity-"+uuid.NewString();pin:=installCapacityTimingFixture(t,ctx,tx,30,60)
 exec(`INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,doctor_report_sha256)
 VALUES($1,true,1000,0,2000,$2)`,node,strings.Repeat("b",64))
 exec(`INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,resource_classes,backends,lock_id,last_seen,capacity,enabled,db_principal,qualified_operations,certificate_sha256,capacity_policy_sha)
 VALUES($1,$2,$3,'["archive"]','["cpu"]','',clock_timestamp()-interval '1 hour','{}',true,session_user,'{"archive.build":["cpu"]}',$4,$5)`,tenant,worker,node,strings.Repeat("c",64),pin)
 report:=func(id uuid.UUID,sha string,want bool){
  t.Helper();exec(`SET LOCAL ROLE photo_worker`);var accepted bool
  err:=tx.QueryRow(ctx,`SELECT photo.heartbeat_worker_capacity_v2($1,'archive','cpu',ARRAY['archive.build'],900,0,1800,$2)`,id,sha).Scan(&accepted)
  exec(`RESET ROLE`);if err!=nil||accepted!=want{t.Fatalf("accepted=%v want=%v err=%v",accepted,want,err)}
 }
 report(uuid.New(),pin,false);report(worker,strings.Repeat("d",64),false)
 var observed *string
 if err=tx.QueryRow(ctx,`SELECT reported_capacity_policy_sha FROM photo.worker_registrations WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker).Scan(&observed);err!=nil||observed!=nil{t.Fatal("foreign report adopted",observed,err)}
 report(worker,pin,true)
 var actual string;var ram,scratch int64;var recorded bool
 if err=tx.QueryRow(ctx,`SELECT reported_capacity_policy_sha,capacity_policy_reported_at IS NOT NULL FROM photo.worker_registrations WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker).Scan(&actual,&recorded);err!=nil||actual!=pin||!recorded{t.Fatal(actual,recorded,err)}
 if err=tx.QueryRow(ctx,`SELECT ram_available_bytes,scratch_available_bytes FROM photo.resource_nodes WHERE node_id=$1`,node).Scan(&ram,&scratch);err!=nil||ram!=900||scratch!=1800{t.Fatal(ram,scratch,err)}
 // Real heartbeat/rotation lifecycle within the rolled-back fixture.
 nextPin:=installCapacityTimingFixture(t,ctx,tx,120,300)
 var changed int;var principal string
 if err=tx.QueryRow(ctx,`SELECT session_user::text`).Scan(&principal);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT photo.rotate_worker_capacity_policy($1,session_user::name,$2,$3,$4)`,worker,node,pin,nextPin).Scan(&changed);err!=nil||changed!=1{t.Fatal("rotation",changed,err)}
 var cleared bool
 if err=tx.QueryRow(ctx,`SELECT capacity_policy_sha=$3 AND reported_capacity_policy_sha IS NULL AND capacity_policy_reported_at IS NULL FROM photo.worker_registrations WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,nextPin).Scan(&cleared);err!=nil||!cleared{t.Fatal("old report retained after rotation",cleared,err)}
 report(worker,pin,false)
 report(worker,nextPin,true)
 var nodeBound bool
 if err=tx.QueryRow(ctx,`SELECT capacity_report_worker_id=$2 AND capacity_report_policy_sha=$3 FROM photo.resource_nodes WHERE node_id=$1`,node,worker,nextPin).Scan(&nodeBound);err!=nil||!nodeBound{t.Fatal("node capacity report unbound",nodeBound,err)}
 exec(`UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '45 seconds' WHERE node_id=$1`,node)
 exec(`SET LOCAL ROLE photo_fn_owner`)
 if err=tx.QueryRow(ctx,`SELECT photo.capacity_policy_node_fresh(n) FROM photo.resource_nodes n WHERE node_id=$1`,node).Scan(&nodeBound);err!=nil||!nodeBound{t.Fatal("custom offline ignored for node",nodeBound,err)}
 exec(`RESET ROLE`)
 for _,age:=range []string{"121 seconds","-3600 seconds"}{
  exec(`UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-$2::interval WHERE node_id=$1`,node,age)
  exec(`SET LOCAL ROLE photo_fn_owner`)
  if err=tx.QueryRow(ctx,`SELECT photo.capacity_policy_node_fresh(n) FROM photo.resource_nodes n WHERE node_id=$1`,node).Scan(&nodeBound);err!=nil||nodeBound{t.Fatal("stale/future node report accepted",age,nodeBound,err)}
  exec(`RESET ROLE`)
 }
 report(worker,nextPin,true)
 var reportedAt time.Time
 if err=tx.QueryRow(ctx,`SELECT capacity_policy_reported_at FROM photo.worker_registrations WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker).Scan(&reportedAt);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT photo.rotate_worker_capacity_policy($1,session_user::name,$2,$3,$3)`,worker,node,nextPin).Scan(&changed);err!=nil||changed!=1{t.Fatal("same pin",changed,err)}
 for _,refusal:=range []struct{worker uuid.UUID;principal,node,expected,next string}{
  {worker,principal,node,pin,nextPin},
  {uuid.New(),principal,node,nextPin,nextPin},
  {worker,"missing-capacity-principal",node,nextPin,nextPin},
  {worker,principal,"missing-capacity-node",nextPin,nextPin},
  {worker,principal,node,nextPin,strings.Repeat("f",64)},
 }{
  exec(`SAVEPOINT refused_policy_rotation`)
  err:=tx.QueryRow(ctx,`SELECT photo.rotate_worker_capacity_policy($1,$2::name,$3,$4,$5)`,refusal.worker,refusal.principal,refusal.node,refusal.expected,refusal.next).Scan(&changed)
  exec(`ROLLBACK TO SAVEPOINT refused_policy_rotation`);exec(`RELEASE SAVEPOINT refused_policy_rotation`)
  if err==nil{t.Fatal("invalid operator rotation accepted",refusal)}
 }
 var unchangedReport bool
 if err=tx.QueryRow(ctx,`SELECT capacity_policy_sha=$3 AND reported_capacity_policy_sha=$3 AND capacity_policy_reported_at=$4 FROM photo.worker_registrations WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,nextPin,reportedAt).Scan(&unchangedReport);err!=nil||!unchangedReport{t.Fatal("no-op/refusal changed report",unchangedReport,err)}
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler"}{
  var granted bool
  if err=tx.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.rotate_worker_capacity_policy(uuid,name,text,text,text)','EXECUTE')`,role).Scan(&granted);err!=nil||granted{t.Fatal("rotation role exposed",role,granted,err)}
 }
 pin=nextPin
 exec(`SET LOCAL ROLE photo_worker`)
 var legacy bool
 if err=tx.QueryRow(ctx,`SELECT photo.heartbeat_cpu_worker($1,'archive',ARRAY['archive.build'],1,1)`,worker).Scan(&legacy);err!=nil||legacy{t.Fatal("legacy bypass",legacy,err)}
 exec(`RESET ROLE`)
 var exposed bool
 if err=tx.QueryRow(ctx,`SELECT has_function_privilege('photo_worker','photo.capacity_heartbeat_cpu_worker_locked(uuid,text,text[],bigint,bigint)','EXECUTE') OR has_function_privilege('photo_api','photo.heartbeat_worker_capacity_v2(uuid,text,text,text[],bigint,bigint,bigint,text)','EXECUTE') OR has_table_privilege('photo_worker','photo.worker_registrations','UPDATE')`).Scan(&exposed);err!=nil||exposed{t.Fatal("capacity ACL",exposed,err)}
 exec(`UPDATE photo.worker_registrations SET enabled=false WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker);report(worker,pin,false)
}


func TestIntegrationCapacityPolicyHeartbeatResourceMatrix(t *testing.T) {
 cases:=[]struct{name,class,backend,operation string;gpuBudget int64;mixed bool}{
  {"cpu","cpu","cpu","asset.inspect",0,false},
  {"media","media","cpu","asset.verify",0,false},
  {"gpu_cpu","gpu","cpu","pair.fit.color",0,false},
  {"mps","gpu","mps","pair.fit.color",1000,false},
  {"cuda","gpu","cuda","pair.fit.color",1000,false},
  {"mixed_cpu","cpu","cpu","asset.inspect",1000,true},
 }
 for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
  dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated DB required")}
  ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
  db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
  exec:=func(query string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,query,args...);err!=nil{t.Fatal(err)}}
  var user,tenant uuid.UUID;var epoch int64
  if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@capacity-matrix.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
  if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,user).Scan(&tenant);err!=nil{t.Fatal(err)}
  worker:=uuid.New();node:="capacity-"+uuid.NewString();pin:=installCapacityTimingFixture(t,ctx,tx,30,60,700,500,300)
  exec(`INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,doctor_report_sha256)
  VALUES($1,true,1000,$2,2000,111,0,222,$3)`,node,tc.gpuBudget,strings.Repeat("b",64))
  exec(`INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,resource_classes,backends,lock_id,last_seen,capacity,enabled,db_principal,qualified_operations,certificate_sha256,capacity_policy_sha)
  VALUES($1,$2,$3,jsonb_build_array($4::text),jsonb_build_array($5::text),'',clock_timestamp()-interval '1 hour','{}',true,session_user,jsonb_build_object($6::text,jsonb_build_array($5::text)),$7,$8)`,tenant,worker,node,tc.class,tc.backend,tc.operation,strings.Repeat("c",64),pin)
  report:=func(operations []string,sha string,want bool){
   t.Helper();exec(`SET LOCAL ROLE photo_worker`);var accepted bool;vram:=int64(0);if tc.backend!="cpu"{vram=3000}
   err:=tx.QueryRow(ctx,`SELECT photo.heartbeat_worker_capacity_v2($1,$2,$3,$4,2000,$5,4000,$6)`,worker,tc.class,tc.backend,operations,vram,sha).Scan(&accepted)
   exec(`RESET ROLE`);if err!=nil||accepted!=want{t.Fatalf("accepted=%v want=%v err=%v",accepted,want,err)}
  }
  authorized:=func(want bool){
   t.Helper();exec(`SET LOCAL ROLE photo_fn_owner`);var accepted bool
   err:=tx.QueryRow(ctx,`SELECT photo.worker_authorized($1,$2,$3,$4,$5,$6,NULL)`,tenant,worker,tc.class,node,tc.operation,tc.backend).Scan(&accepted)
   exec(`RESET ROLE`);if err!=nil||accepted!=want{t.Fatalf("task authorized=%v want=%v err=%v",accepted,want,err)}
  }
  authorized(false)
  report([]string{"foreign.operation"},pin,false)
  report([]string{tc.operation},strings.Repeat("d",64),false)
  report([]string{tc.operation},pin,true)
  authorized(true)
  exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp()-interval '31 seconds',last_seen=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
  authorized(false)
  report([]string{tc.operation},pin,true)
  authorized(true)
  var ram,vram,scratch int64
  if err=tx.QueryRow(ctx,`SELECT ram_available_bytes,vram_available_bytes,scratch_available_bytes FROM photo.resource_nodes WHERE node_id=$1`,node).Scan(&ram,&vram,&scratch);err!=nil{t.Fatal(err)}
  if tc.mixed {if ram!=111||vram!=0||scratch!=222{t.Fatal("CPU overwrote mixed node capacity",ram,vram,scratch)}} else {wantRAM:=int64(500);if tc.class=="cpu"{wantRAM=300};wantVRAM:=tc.gpuBudget;if wantVRAM>700{wantVRAM=700};if ram!=wantRAM||vram!=wantVRAM||scratch!=2000{t.Fatal("capacity not policy capped",ram,vram,scratch)}}
  exec(`UPDATE photo.worker_registrations SET capacity_policy_sha=$3 WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,strings.Repeat("e",64))
  authorized(false)
  report([]string{tc.operation},pin,false)
  report([]string{tc.operation},strings.Repeat("e",64),false)
  nextPin:=installCapacityTimingFixture(t,ctx,tx,30,61,700,500,300)
  exec(`UPDATE photo.worker_registrations SET capacity_policy_sha=$3 WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,nextPin)
  report([]string{tc.operation},nextPin,true)
  authorized(true)
 })}
}


func TestIntegrationCapacityPolicyStyleRuntimeSelection(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated DB required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 exec:=func(query string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,query,args...);err!=nil{t.Fatal(err)}}
 var user,tenant uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@capacity-selection.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,user).Scan(&tenant);err!=nil{t.Fatal(err)}
 worker:=uuid.New();pin:=installCapacityTimingFixture(t,ctx,tx,30,60);lock:=strings.Repeat("b",64)
 exec(`INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,enabled,db_principal,qualified_operations,capacity_policy_sha)
 VALUES($1,$2,'["cpu"]','["cpu"]',$3,clock_timestamp(),'{}',true,session_user,'{"style.aggregate":["cpu"],"mask.face_skin":["cpu"],"pair.fit.beauty":["cpu"],"pair.fit.defects":["cpu"],"style.validate":["cpu"],"style.package":["cpu"]}',$4)`,tenant,worker,lock,pin)
 selectRuntime:=func(want string){
  t.Helper();exec(`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());exec(`SET LOCAL ROLE photo_fn_owner`)
  var fit,backend,aggregate string
  err:=tx.QueryRow(ctx,`SELECT fit_lock_id,fit_backend,aggregate_lock_id FROM photo.select_style_analysis_runtimes($1,false)`,tenant).Scan(&fit,&backend,&aggregate)
  exec(`RESET ROLE`);if err!=nil||aggregate!=want{t.Fatalf("aggregate=%q want=%q err=%v",aggregate,want,err)}
 }
 selectRuntime("")
 exec(`UPDATE photo.worker_registrations SET reported_capacity_policy_sha=$3,capacity_policy_reported_at=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,pin)
 selectRuntime(lock)
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp()-interval '31 seconds',last_seen=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
 selectRuntime("")
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp(),reported_capacity_policy_sha=$3 WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,strings.Repeat("c",64))
 selectRuntime("")
}


// Exercises scheduler selection against a persisted ready task, not only the authority helper.
func TestIntegrationCapacityPolicyDispatchSelection(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated DB required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 exec:=func(query string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,query,args...);err!=nil{t.Fatal(err)}}
 var user,tenant,params,snapshot,run uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@capacity-dispatch.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,user).Scan(&tenant);err!=nil{t.Fatal(err)}
 hash:=strings.Repeat("b",64);pin:=installCapacityTimingFixture(t,ctx,tx,30,20,10000000000,17179869184,8589934592,1,1)
 if err=tx.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id`,tenant,uuid.NewString(),hash).Scan(&params);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id`,tenant,user,hash).Scan(&snapshot);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'apply','running',$2,'{}',$3,$4,true) RETURNING id`,tenant,snapshot,hash,user).Scan(&run);err!=nil{t.Fatal(err)}
 task:=uuid.New();worker:=uuid.New();node:="capacity-dispatch-"+uuid.NewString()
 exec(`INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES($1,$2,$3,'dispatch','asset.verify','1','media','ready','[]',$4,$5,'{"backend":"cpu","resource_estimate":{"ram_bytes":2048,"vram_bytes":0,"scratch_bytes":1024,"output_bytes":0,"pixel_count":0}}')`,task,tenant,run,params,hash)
 exec(`INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES($1,true,10000000,0,10000000,10000000,10000000,clock_timestamp(),$2)`,node,hash)
 exec(`INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES($1,'media1','media','{}')`,node)
 exec(`INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,capacity_policy_sha)
 VALUES($1,$2,'["media"]','["cpu"]',$3,clock_timestamp(),'{}',session_user,$4,true,'{"asset.verify":["cpu"]}',$5)`,tenant,worker,hash,node,pin)
 selected:=func(want bool){
  t.Helper();exec(`SET LOCAL ROLE photo_scheduler`);var found bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.eligible_dispatch_nodes($1) WHERE node_id=$2)`,task,node).Scan(&found)
  exec(`RESET ROLE`);if err!=nil||found!=want{t.Fatalf("dispatch selection=%v want=%v err=%v",found,want,err)}
 }
 refusedReservation:=func(){
  t.Helper();event:=uuid.New()
  exec(`SET LOCAL ROLE photo_scheduler`)
  var outcome string;var command []byte
  err:=tx.QueryRow(ctx,`SELECT outcome,command FROM photo.reserve_dispatch($1,$2,2048,0,1024,$3,$4,'')`,task,node,event,uuid.New()).Scan(&outcome,&command)
  exec(`RESET ROLE`)
  if err!=nil||outcome!="waiting_worker"||command!=nil{t.Fatalf("reservation outcome=%q command=%s err=%v",outcome,command,err)}
  var state string;var occupied,reserved,published bool
  if err=tx.QueryRow(ctx,`SELECT state,
   EXISTS(SELECT 1 FROM photo.resource_slots WHERE node_id=$2 AND task_id=$1),
   EXISTS(SELECT 1 FROM photo.resource_reservations WHERE task_id=$1 AND state='reserved'),
   EXISTS(SELECT 1 FROM photo.outbox WHERE tenant_id=$3 AND event_id=$4)
   FROM photo.tasks WHERE id=$1`,task,node,tenant,event).Scan(&state,&occupied,&reserved,&published);err!=nil{t.Fatal(err)}
  if state!="ready"||occupied||reserved||published{t.Fatalf("refused reservation mutated dispatch: state=%s occupied=%v reserved=%v published=%v",state,occupied,reserved,published)}
 }
 selected(false)
 refusedReservation()
 exec(`UPDATE photo.worker_registrations SET reported_capacity_policy_sha=$3,capacity_policy_reported_at=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,pin)
 selected(true)
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp()-interval '31 seconds',last_seen=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
 selected(false)
 refusedReservation()
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp(),reported_capacity_policy_sha=$3 WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,hash)
 selected(false)
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp()+interval '1 hour',reported_capacity_policy_sha=$3 WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,pin)
 selected(false)
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
 selected(true)
 // Successful admission is isolated so legacy selection below still sees a free slot.
 exec(`SAVEPOINT fresh_policy_reservation`)
 // The operator multiplier must affect real admission and persisted bytes.
 // 1024 raw bytes fit, but ceil(1024*1.2)=1229 policy bytes do not.
 exec(`UPDATE photo.resource_nodes SET capacity_report_worker_id=$2,capacity_report_policy_sha=$3,scratch_available_bytes=1024 WHERE node_id=$1`,node,worker,pin)
 scratchEvent:=uuid.New()
 exec(`SET LOCAL ROLE photo_scheduler`)
 var scratchOutcome string;var scratchCommand []byte
 err=tx.QueryRow(ctx,`SELECT outcome,command FROM photo.reserve_dispatch($1,$2,2048,0,1024,$3,$4,'')`,task,node,scratchEvent,uuid.New()).Scan(&scratchOutcome,&scratchCommand)
 exec(`RESET ROLE`)
 if err!=nil||scratchOutcome!="waiting_resource"||scratchCommand!=nil{t.Fatal("scratch multiplier refusal",scratchOutcome,err)}
 var scratchUnchanged bool
 if err=tx.QueryRow(ctx,`SELECT t.state='ready' AND t.attempt=0 AND t.dispatch_worker_id IS NULL
 AND t.execution_profile->'resource_estimate'->>'scratch_bytes'='1024'
 AND NOT EXISTS(SELECT 1 FROM photo.resource_reservations rr WHERE rr.task_id=t.id)
 AND NOT EXISTS(SELECT 1 FROM photo.resource_slots s WHERE s.node_id=$2 AND s.task_id IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM photo.outbox o WHERE o.tenant_id=t.tenant_id AND o.event_id=$3)
 FROM photo.tasks t WHERE t.id=$1`,task,node,scratchEvent).Scan(&scratchUnchanged);err!=nil||!scratchUnchanged{t.Fatal("scratch refusal mutated task/resources",err)}
 exec(`UPDATE photo.resource_nodes SET scratch_available_bytes=10000000 WHERE node_id=$1`,node)
 event:=uuid.New();correlation:=uuid.New()
 exec(`SET LOCAL ROLE photo_scheduler`)
 var outcome string;var command []byte
 err=tx.QueryRow(ctx,`SELECT outcome,command FROM photo.reserve_dispatch($1,$2,2048,0,1024,$3,$4,'')`,task,node,event,correlation).Scan(&outcome,&command)
 exec(`RESET ROLE`)
 if err!=nil||outcome!="queued"||len(command)==0{t.Fatalf("fresh reservation outcome=%q err=%v",outcome,err)}
 var state,routing string;var target uuid.UUID;var occupied,reserved,envelopeMatches bool
 if err=tx.QueryRow(ctx,`SELECT t.state,t.dispatch_worker_id,o.routing_key,
 EXISTS(SELECT 1 FROM photo.resource_slots WHERE node_id=$2 AND task_id=t.id AND tenant_id=t.tenant_id),
 EXISTS(SELECT 1 FROM photo.resource_reservations WHERE task_id=t.id AND tenant_id=t.tenant_id AND node_id=$2 AND state='reserved' AND ram_bytes=2048 AND vram_bytes=0 AND scratch_bytes=1229),
 t.execution_profile->'resource_estimate'->>'scratch_bytes'='1024' AND t.dispatch_command=o.payload AND t.dispatch_command=$4::jsonb
 FROM photo.tasks t JOIN photo.outbox o ON o.tenant_id=t.tenant_id AND o.event_id=$3
 WHERE t.id=$1`,task,node,event,string(command)).Scan(&state,&target,&routing,&occupied,&reserved,&envelopeMatches);err!=nil{t.Fatal(err)}
 if state!="queued"||target!=worker||!strings.HasPrefix(routing,"worker."+worker.String()+".")||!occupied||!reserved||!envelopeMatches{
  t.Fatalf("fresh dispatch state=%s target=%s routing=%s occupied=%v reserved=%v envelope=%v",state,target,routing,occupied,reserved,envelopeMatches)
 }
 // The physical policy report was bound before reservation above.
 exec(`SET LOCAL ROLE photo_fn_owner`)
 for _,class:=range []string{"cpu","media","archive","gpu"}{
  var available bool
  if err=tx.QueryRow(ctx,`SELECT photo.capacity_policy_node_concurrency_available(n,$2) FROM photo.resource_nodes n WHERE node_id=$1`,node,class).Scan(&available);err!=nil||available!=(class=="gpu"){t.Fatal("shared CPU concurrency",class,available,err)}
 }
 exec(`RESET ROLE`)
 // A spare physical slot and ample bytes cannot bypass the shared CPU cap.
 secondTask,secondEvent:=uuid.New(),uuid.New()
 exec(`INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES($1,'media2','media','{}')`,node)
 exec(`INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 SELECT $2,tenant_id,run_id,'dispatch-second',operation,operation_version,resource_class,'ready',inputs,params_artifact_id,input_digest,execution_profile FROM photo.tasks WHERE id=$1`,task,secondTask)
 exec(`SET LOCAL ROLE photo_scheduler`)
 var secondEligible bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.eligible_dispatch_nodes($1) WHERE node_id=$2)`,secondTask,node).Scan(&secondEligible);err!=nil||secondEligible{t.Fatal("CPU cap ignored in candidate selection",secondEligible,err)}
 var secondOutcome string;var secondCommand []byte
 err=tx.QueryRow(ctx,`SELECT outcome,command FROM photo.reserve_dispatch($1,$2,2048,0,1024,$3,$4,'')`,secondTask,node,secondEvent,uuid.New()).Scan(&secondOutcome,&secondCommand)
 exec(`RESET ROLE`)
 if err!=nil||secondOutcome!="waiting_resource"||secondCommand!=nil{t.Fatal("CPU cap reservation",secondOutcome,string(secondCommand),err)}
 var secondUntouched bool
 if err=tx.QueryRow(ctx,`SELECT t.state='ready' AND t.dispatch_command IS NULL AND t.dispatch_worker_id IS NULL AND t.attempt=0
 AND NOT EXISTS(SELECT 1 FROM photo.resource_reservations rr WHERE rr.task_id=t.id)
 AND NOT EXISTS(SELECT 1 FROM photo.outbox o WHERE o.tenant_id=t.tenant_id AND o.event_id=$3)
 AND EXISTS(SELECT 1 FROM photo.resource_slots s WHERE s.node_id=$2 AND s.slot_name='media2' AND s.task_id IS NULL)
 AND (SELECT count(*) FROM photo.resource_reservations rr WHERE rr.node_id=$2 AND rr.state='reserved')=1
 FROM photo.tasks t WHERE t.id=$1`,secondTask,node,secondEvent).Scan(&secondUntouched);err!=nil||!secondUntouched{t.Fatal("refused CPU admission mutated state",secondUntouched,err)}
 // A fresh dispatch does not authorize a later claim with an expired report.
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp()-interval '31 seconds',last_seen=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
 assertLease:=func(value time.Time,started time.Time,capped bool){
  t.Helper();var now time.Time;var synchronized,atDeadline bool
  if err=tx.QueryRow(ctx,`SELECT clock_timestamp(),t.lease_until=$2 AND t.lease_until<=t.deadline
   AND EXISTS(SELECT 1 FROM photo.resource_slots s WHERE s.task_id=t.id AND s.tenant_id=t.tenant_id AND s.lease_until=$2 AND s.fencing_token=t.fencing_token)
   AND EXISTS(SELECT 1 FROM photo.resource_reservations rr WHERE rr.task_id=t.id AND rr.tenant_id=t.tenant_id AND rr.state='reserved' AND rr.expires_at=$2 AND rr.fencing_token=t.fencing_token AND rr.scratch_bytes=1229)
   AND t.execution_profile->'resource_estimate'->>'scratch_bytes'='1024',
   t.lease_until=t.deadline FROM photo.tasks t WHERE t.id=$1`,task,value).Scan(&now,&synchronized,&atDeadline);err!=nil{t.Fatal(err)}
  if !synchronized{t.Fatal("task/slot/reservation lease or fence disagreement")}
  if capped{if !atDeadline{t.Fatal("renewal did not stop at exact task deadline")};return}
  if value.Before(started.Add(20*time.Second))||value.After(now.Add(20*time.Second)){t.Fatalf("lease duration does not follow20-second policy: start=%v lease=%v now=%v",started,value,now)}
 }
 claim:=func(want string)int64{
  t.Helper();var started time.Time
  if err=tx.QueryRow(ctx,`SELECT clock_timestamp()`).Scan(&started);err!=nil{t.Fatal(err)}
  exec(`SET LOCAL ROLE photo_worker`)
  var actual string;var fence *int64;var lease,deadline *time.Time;var contextBytes []byte
  err:=tx.QueryRow(ctx,`SELECT outcome,fence,lease_until,deadline,context FROM photo.claim_task($1,1,$2,$3::jsonb)`,task,worker,string(command)).Scan(&actual,&fence,&lease,&deadline,&contextBytes)
  exec(`RESET ROLE`)
  if err!=nil||actual!=want{t.Fatalf("claim=%q want=%q err=%v",actual,want,err)}
  if want!="claimed"{if fence!=nil||lease!=nil||deadline!=nil||contextBytes!=nil{t.Fatal("refused claim returned lease")};return 0}
  if fence==nil||*fence<=0||lease==nil||deadline==nil||len(contextBytes)==0{t.Fatal("claimed task missing lease context")};assertLease(*lease,started,false);return *fence
 }
 claim("unavailable")
 var untouched bool
 if err=tx.QueryRow(ctx,`SELECT state='queued' AND attempt=0 AND worker_id IS NULL AND lease_until IS NULL AND NOT EXISTS(SELECT 1 FROM photo.task_attempts WHERE task_id=$1) FROM photo.tasks WHERE id=$1`,task).Scan(&untouched);err!=nil||!untouched{t.Fatal("refused claim changed attempt",untouched,err)}
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
 fence:=claim("claimed")
 deadlineCapped:=false
 heartbeat:=func(want bool){
  t.Helper();var started time.Time
  if err=tx.QueryRow(ctx,`SELECT clock_timestamp()`).Scan(&started);err!=nil{t.Fatal(err)}
  exec(`SET LOCAL ROLE photo_worker`)
  var accepted,cancelled bool;var lease *time.Time
  err:=tx.QueryRow(ctx,`SELECT accepted,lease_until,cancel_requested FROM photo.heartbeat_task($1,1,$2,$3,'{"step":"policy-check"}')`,task,fence,worker).Scan(&accepted,&lease,&cancelled)
  exec(`RESET ROLE`)
  if err!=nil||accepted!=want{t.Fatalf("renew accepted=%v want=%v err=%v",accepted,want,err)}
  if want&&(lease==nil||cancelled){t.Fatal("invalid renewed lease")};if !want&&lease!=nil{t.Fatal("refused renewal returned lease")}
  if want{assertLease(*lease,started,deadlineCapped)}
 }
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp()-interval '31 seconds',last_seen=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
 var beforeLease time.Time
 if err=tx.QueryRow(ctx,`SELECT lease_until FROM photo.tasks WHERE id=$1`,task).Scan(&beforeLease);err!=nil{t.Fatal(err)}
 heartbeat(false)
 var unchanged bool
 if err=tx.QueryRow(ctx,`SELECT lease_until=$2 AND progress='{}'::jsonb FROM photo.tasks WHERE id=$1`,task,beforeLease).Scan(&unchanged);err!=nil||!unchanged{t.Fatal("refused renewal changed task",unchanged,err)}
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
 heartbeat(true)
 // No sleep: shorten the authoritative task deadline and verify exact clipping.
 exec(`UPDATE photo.tasks SET deadline=clock_timestamp()+interval '5 seconds' WHERE id=$1`,task)
 deadlineCapped=true
 heartbeat(true)
 // Private canonical release uses persisted fence/bytes, never re-estimates scratch.
 exec(`SET LOCAL ROLE photo_fn_owner`)
 exec(`SELECT photo.release_task_resources($1,$2,$3)`,tenant,task,fence+1)
 exec(`RESET ROLE`)
 var wrongFenceRetained bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.resource_reservations WHERE tenant_id=$1 AND task_id=$2 AND state='reserved' AND fencing_token=$3 AND scratch_bytes=1229)
 AND EXISTS(SELECT 1 FROM photo.resource_slots WHERE tenant_id=$1 AND task_id=$2 AND fencing_token=$3)`,tenant,task,fence).Scan(&wrongFenceRetained);err!=nil||!wrongFenceRetained{t.Fatal("wrong fence released scratch reservation",err)}
 exec(`SET LOCAL ROLE photo_fn_owner`)
 exec(`SELECT photo.release_task_resources($1,$2,$3)`,tenant,task,fence)
 exec(`SELECT photo.release_task_resources($1,$2,$3)`,tenant,task,fence)
 var released,available bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.resource_reservations WHERE tenant_id=$1 AND task_id=$2 AND state='released' AND fencing_token=$3 AND scratch_bytes=1229)
 AND NOT EXISTS(SELECT 1 FROM photo.resource_slots WHERE tenant_id=$1 AND task_id=$2),
 photo.capacity_policy_node_concurrency_available(n,'media') FROM photo.resource_nodes n WHERE node_id=$4`,tenant,task,fence,node).Scan(&released,&available);err!=nil||!released||!available{t.Fatal("idempotent release retained node admission",released,available,err)}
 exec(`RESET ROLE`)
 exec(`ROLLBACK TO SAVEPOINT fresh_policy_reservation`)
 exec(`RELEASE SAVEPOINT fresh_policy_reservation`)
 // Legacy registration remains compatible until an operator pins its policy.
 exec(`UPDATE photo.worker_registrations SET capacity_policy_sha=NULL,reported_capacity_policy_sha=NULL,capacity_policy_reported_at=NULL WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
 selected(true)
}

func installCapacityTimingFixture(t *testing.T,ctx context.Context,tx pgx.Tx,offline,lease int,budgets ...int64)string{
 t.Helper();raw,err:=os.ReadFile("../../../configs/capacity-metal-verification.json");if err!=nil{t.Fatal(err)}
 var policy map[string]any;if err=json.Unmarshal(raw,&policy);err!=nil{t.Fatal(err)}
 policy["offline_seconds"]=offline;policy["lease_seconds"]=lease
 if len(budgets)!=0{if len(budgets)!=3&&len(budgets)!=5{t.Fatal("expected GPU/photo/control budgets with optional GPU/CPU concurrency")};policy["gpu_budget_bytes"]=budgets[0];policy["photo_ram_budget_bytes"]=budgets[1];policy["control_ram_budget_bytes"]=budgets[2];if len(budgets)==5{policy["gpu_concurrency"]=budgets[3];policy["cpu_concurrency"]=budgets[4]}}
 raw,err=canonical.Marshal(policy);if err!=nil{t.Fatal(err)};sum:=sha256.Sum256(raw);pin:=hex.EncodeToString(sum[:])
 if _,err=tx.Exec(ctx,`INSERT INTO photo.capacity_policy_timings(policy_sha,policy_jcs,offline_seconds,lease_seconds) VALUES($1,$2,$3,$4) ON CONFLICT(policy_sha) DO NOTHING`,pin,string(raw),offline,lease);err!=nil{t.Fatal(err)}
 return pin
}

func TestIntegrationCapacityPolicyCustomTimingAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrated through01632 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 exec:=func(q string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,q,args...);err!=nil{t.Fatal(err)}}
 var user,tenant uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@capacity-custom.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,user).Scan(&tenant);err!=nil{t.Fatal(err)}
 worker:=uuid.New();pin:=installCapacityTimingFixture(t,ctx,tx,120,300)
 exec(`INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,enabled,db_principal,qualified_operations,capacity_policy_sha,reported_capacity_policy_sha,capacity_policy_reported_at)
 VALUES($1,$2,'["cpu"]','["cpu"]','',clock_timestamp()-interval '45 seconds','{}',true,session_user,'{"asset.inspect":["cpu"]}',$3,$3,clock_timestamp()-interval '45 seconds')`,tenant,worker,pin)
 fresh:=func(want bool){t.Helper();exec(`SET LOCAL ROLE photo_fn_owner`);var actual bool
  err:=tx.QueryRow(ctx,`SELECT photo.capacity_policy_worker_fresh(w) FROM photo.worker_registrations w WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker).Scan(&actual)
  exec(`RESET ROLE`);if err!=nil||actual!=want{t.Fatal(actual,want,err)}
 }
 fresh(true)
 exec(`SET LOCAL ROLE photo_worker`)
 for _,probe:=range []struct{worker uuid.UUID;sha string;offline,lease int;want bool}{
  {worker,pin,120,300,true},{uuid.New(),pin,120,300,false},
  {worker,pin,30,300,false},{worker,pin,120,60,false},{worker,strings.Repeat("f",64),120,300,false},
 }{
  var ready bool
  if err=tx.QueryRow(ctx,`SELECT photo.capacity_policy_timing_ready($1,$2,$3,$4)`,probe.worker,probe.sha,probe.offline,probe.lease).Scan(&ready);err!=nil||ready!=probe.want{t.Fatal("startup timing",ready,probe.want,err)}
 }
 exec(`RESET ROLE`)
 exec(`SET LOCAL ROLE photo_fn_owner`);var seconds int
 if err=tx.QueryRow(ctx,`SELECT photo.capacity_policy_task_lease_seconds($1,$2,1000)`,tenant,worker).Scan(&seconds);err!=nil||seconds!=300{t.Fatal(seconds,err)}
 if err=tx.QueryRow(ctx,`SELECT photo.capacity_policy_task_lease_seconds($1,$2,20)`,tenant,worker).Scan(&seconds);err!=nil||seconds!=20{t.Fatal(seconds,err)}
 exec(`RESET ROLE`)
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp()-interval '121 seconds' WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker);fresh(false)
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp(),last_seen=clock_timestamp()-interval '121 seconds' WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker);fresh(false)
 exec(`UPDATE photo.worker_registrations SET capacity_policy_reported_at=clock_timestamp()+interval '1 hour',last_seen=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker);fresh(false)
 exec(`UPDATE photo.worker_registrations SET capacity_policy_sha=$3,reported_capacity_policy_sha=$3,capacity_policy_reported_at=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker,strings.Repeat("f",64));fresh(false)
 var exposed bool
 if err=tx.QueryRow(ctx,`SELECT has_table_privilege('photo_worker','photo.capacity_policy_timings','INSERT') OR has_table_privilege('photo_scheduler','photo.capacity_policy_timings','UPDATE') OR has_function_privilege('photo_worker','photo.capacity_policy_task_lease_seconds(uuid,uuid,integer)','EXECUTE')`).Scan(&exposed);err!=nil||exposed{t.Fatal("timing ACL",exposed,err)}
}
