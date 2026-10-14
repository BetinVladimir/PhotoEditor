package workflow

import (
 "context"
 "errors"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
)

// Requires an isolated database migrated through 01787. This is installation
// evidence only; physical backlog/fairness behavior needs separate fixtures.
func TestIntegrationCapacityHeadFunctionOwnershipAndScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated operator database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 cases:=[]struct{signature string; scheduler bool; needles []string}{
  {"photo.capacity_queue_charge(bigint,bigint)",false,[]string{"INVALID_CAPACITY_CHARGE"}},
  {"photo.capacity_queue_head_snapshot(text,timestamptz,bigint,bigint)",true,[]string{"queued_heavy AS MATERIALIZED","eligible_dispatch_nodes(t.id)","PARTITION BY r.tenant_id,r.effective_queue","t.admission_after<=p_now"}},
  {"photo.capacity_queue_tenant_candidates(text,uuid,timestamptz)",false,[]string{"t.tenant_id=p_tenant","eligible_dispatch_nodes(t.id)","t.admission_after<=p_now"}},
  {"photo.capacity_queue_head_affinity(text,uuid,uuid,text,timestamptz,timestamptz,bigint,uuid[])",true,[]string{"capacity_queue_tenant_candidates(p_pool,p_tenant,p_now)","capacity_task_affinity(ARRAY[c.task_id])","c.task_id<>ALL(p_excluded)","NOT EXISTS (", "earlier.created_at,earlier.task_id"}},
  {"photo.capacity_queue_bounded_head_choice(text,timestamptz,bigint,bigint,uuid,integer,bigint[],uuid[])",true,[]string{"capacity_queue_head_snapshot(","WITH heads AS MATERIALIZED","h.task_id<>ALL(p_excluded_heads)","h.tenant_id>p_last_tenant","LIMIT 1"}},
 }
 for _,tc:=range cases{t.Run(tc.signature,func(t *testing.T){
  var owner,definition string;var secdef,api,worker,scheduler bool
  err:=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),
 has_function_privilege('photo_api',p.oid,'EXECUTE'),
 has_function_privilege('photo_worker',p.oid,'EXECUTE'),
 has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure($1)`,tc.signature).Scan(&owner,&secdef,&definition,&api,&worker,&scheduler)
  if err!=nil{t.Fatal(err)}
  if owner!="photo_fn_owner"||!secdef||api||worker||scheduler!=tc.scheduler{t.Fatal("invalid capacity head function authority",owner,secdef,api,worker,scheduler)}
  for _,needle:=range tc.needles{if !strings.Contains(definition,needle){t.Fatal("missing installed capacity scope",needle)}}
 })}
}

func TestIntegrationCapacityHeadIndexesReady(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01785 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 for _,tc:=range []struct{name,columns,predicate string}{
  {"capacity_ready_tenant_created_idx","(tenant_id, created_at, id)","state = 'ready'"},
  {"capacity_queued_gpu_tenant_idx","(tenant_id)","state = 'queued'"},
 }{t.Run(tc.name,func(t *testing.T){
  var valid,ready bool;var definition,predicate string
  err:=conn.QueryRow(ctx,`SELECT x.indisvalid,x.indisready,pg_get_indexdef(i.oid),pg_get_expr(x.indpred,x.indrelid)
 FROM pg_class i JOIN pg_namespace n ON n.oid=i.relnamespace
 JOIN pg_index x ON x.indexrelid=i.oid
 WHERE n.nspname='photo' AND i.relname=$1`,tc.name).Scan(&valid,&ready,&definition,&predicate)
  if err!=nil{t.Fatal(err)}
  if !valid||!ready||!strings.Contains(definition,tc.columns)||!strings.Contains(predicate,tc.predicate){t.Fatal("invalid capacity index",definition,predicate,valid,ready)}
  if tc.name=="capacity_queued_gpu_tenant_idx"&&!strings.Contains(predicate,"resource_class = 'gpu'"){t.Fatal("non-GPU rows included",predicate)}
 })}
}

// An ancient cycle timestamp gives an empty eligible set even when the
// isolated database contains current tasks. The bounded selector must still
// return its aggregate row: Go needs the cap to start an empty cycle safely.
func TestIntegrationCapacityBoundedHeadEmptyCycle(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01787 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 var taskID,tenantID *string
 var capSeconds int64
 err=conn.QueryRow(ctx,`SELECT task_id::text,tenant_id::text,max_runnable_cost_seconds
 FROM photo.capacity_queue_bounded_head_choice(
  'cpu',$1,30,2,'00000000-0000-0000-0000-000000000000'::uuid,0,
  ARRAY[120,90,60,30]::bigint[],ARRAY[]::uuid[])`,time.Date(1970,1,1,0,0,0,0,time.UTC)).
  Scan(&taskID,&tenantID,&capSeconds)
 if err!=nil{t.Fatal(err)}
 if taskID!=nil||tenantID!=nil||capSeconds!=3600{t.Fatal("empty bounded cycle lost its aggregate cap",taskID,tenantID,capSeconds)}
}

func TestIntegrationCapacityHeadExclusionsRejectInvalidArrays(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01787 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 for _,tc:=range []struct{name,query string;sqlstate string}{
  {"bounded multidimensional",`SELECT count(*) FROM photo.capacity_queue_bounded_head_choice(
   'cpu',$1,30,2,'00000000-0000-0000-0000-000000000000'::uuid,0,
   ARRAY[120,90,60,30]::bigint[],ARRAY[['11111111-1111-1111-1111-111111111111'::uuid]])`,""},
  {"bounded nil UUID",`SELECT count(*) FROM photo.capacity_queue_bounded_head_choice(
   'cpu',$1,30,2,'00000000-0000-0000-0000-000000000000'::uuid,0,
   ARRAY[120,90,60,30]::bigint[],ARRAY['00000000-0000-0000-0000-000000000000'::uuid])`,"22023"},
  {"affinity multidimensional",`SELECT count(*) FROM photo.capacity_queue_head_affinity(
   'cpu','11111111-1111-1111-1111-111111111111'::uuid,
   '22222222-2222-2222-2222-222222222222'::uuid,'interactive',$1,$1,2,
   ARRAY[['33333333-3333-3333-3333-333333333333'::uuid]])`,""},
  {"affinity nil UUID",`SELECT count(*) FROM photo.capacity_queue_head_affinity(
   'cpu','11111111-1111-1111-1111-111111111111'::uuid,
   '22222222-2222-2222-2222-222222222222'::uuid,'interactive',$1,$1,2,
   ARRAY['00000000-0000-0000-0000-000000000000'::uuid])`,"22023"},
 }{t.Run(tc.name,func(t *testing.T){
  var count int
  err:=conn.QueryRow(ctx,tc.query,time.Date(1970,1,1,0,0,0,0,time.UTC)).Scan(&count)
  var pg *pgconn.PgError
  if !errors.As(err,&pg)||(tc.sqlstate!=""&&pg.Code!=tc.sqlstate){t.Fatal("invalid exclusion was accepted or refused with the wrong SQLSTATE",count,err)}
 })}
}
