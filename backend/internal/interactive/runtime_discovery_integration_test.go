package interactive

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
)

// A real, separate worker principal is essential: API discovery must not use
// the session_user-bound claim predicate. Every fixture is rolled back.
func TestIntegrationInteractiveDiscoveryDistinctWorkerAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,mode:=range []string{"qualified_distinct_principal","disabled_node","expired_node","disabled_worker","expired_worker","qualification_withdrawn","foreign_actor","foreign_tenant"}{t.Run(mode,func(t *testing.T){
 tx,e:=db.Begin(ctx);if e!=nil{t.Fatal(e)};defer tx.Rollback(context.WithoutCancel(ctx))
 var actor,tid,session,project uuid.UUID;var epoch int64
 if e=tx.QueryRow(ctx,"SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@test.example").Scan(&actor,&epoch);e!=nil{t.Fatal(e)}
 if e=tx.QueryRow(ctx,"SELECT id FROM photo.tenants WHERE owner_user_id=$1",actor).Scan(&tid);e!=nil{t.Fatal(e)}
 if e=tx.QueryRow(ctx,"INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id",actor,auth.HashSecret(uuid.NewString()),uuid.New()).Scan(&session);e!=nil{t.Fatal(e)}
 if e=tx.QueryRow(ctx,"INSERT INTO photo.projects(tenant_id,name) VALUES($1,'discovery') RETURNING id",tid).Scan(&project);e!=nil{t.Fatal(e)}
 node,worker:="discovery-"+uuid.NewString(),uuid.New();lock:=strings.Repeat("a",64)
 if _,e=tx.Exec(ctx,`INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES($1,true,8589934592,8589934592,8589934592,8589934592,8589934592,clock_timestamp(),$2)`,node,lock);e!=nil{t.Fatal(e)}
 if _,e=tx.Exec(ctx,`INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations) VALUES($1,$2,'["gpu"]','["cpu"]',$3,clock_timestamp(),'{}','photo_worker',$4,true,'{"mask.prompt":["cpu"]}')`,tid,worker,lock,node);e!=nil{t.Fatal(e)}
 var principalDistinct bool
 if e=tx.QueryRow(ctx,"SELECT session_user<>'photo_worker'").Scan(&principalDistinct);e!=nil||!principalDistinct{t.Fatal("fixture needs administrator distinct from worker",e)}
 switch mode{
 case "disabled_node":_,e=tx.Exec(ctx,"UPDATE photo.resource_nodes SET enabled=false WHERE node_id=$1",node)
 case "expired_node":_,e=tx.Exec(ctx,"UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '1 day' WHERE node_id=$1",node)
 case "disabled_worker":_,e=tx.Exec(ctx,"UPDATE photo.worker_registrations SET enabled=false WHERE tenant_id=$1 AND worker_id=$2",tid,worker)
 case "expired_worker":_,e=tx.Exec(ctx,"UPDATE photo.worker_registrations SET last_seen=clock_timestamp()-interval '1 day' WHERE tenant_id=$1 AND worker_id=$2",tid,worker)
 case "qualification_withdrawn":_,e=tx.Exec(ctx,"UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE tenant_id=$1 AND worker_id=$2",tid,worker)
 case "foreign_actor":actor=uuid.New()
 };if e!=nil{t.Fatal(e)}
 if _,e=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tid.String());e!=nil{t.Fatal(e)}
 if _,e=tx.Exec(ctx,"SET LOCAL ROLE photo_api");e!=nil{t.Fatal(e)}
 queryTenant:=tid;if mode=="foreign_tenant"{queryTenant=uuid.New()}
 var count int;var backend,gotLock string
 if e=tx.QueryRow(ctx,`SELECT count(*),coalesce(min(backend),''),coalesce(min(lock_id),'') FROM photo.select_interactive_runtime($1,$2,$3,$4,'mask.prompt')`,queryTenant,project,actor,session).Scan(&count,&backend,&gotLock);e!=nil{t.Fatal(e)}
 if mode=="qualified_distinct_principal"{if count!=1||backend!="cpu"||gotLock!=lock{t.Fatal("distinct worker was hidden",count,backend,gotLock)}}else if count!=0{t.Fatal("unqualified or unauthorized candidate exposed",mode,count)}
 })}
}
