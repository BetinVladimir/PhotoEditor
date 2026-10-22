package run
import("context";"encoding/json";"os";"testing";"time";"github.com/google/uuid";"github.com/jackc/pgx/v5")
// Use only an isolated, migration-complete DB and a genuinely qualified installed
// fixture with exactly one eligible CPU worker for both operations. This test
// never fabricates qualification reports/activation evidence.
func TestIntegrationEditorCapabilityReadinessLiveAuthority(t *testing.T){
 dsn,raw:=os.Getenv("PHOTO_TEST_DATABASE_URL"),os.Getenv("PHOTO_TEST_EDITOR_READINESS_FIXTURE");if dsn==""||raw==""{t.Skip("isolated migrated02548 DB and installed qualified editor fixture required")}
 var f struct{Tenant,Project,Actor,Session,Worker uuid.UUID;Node string};if json.Unmarshal([]byte(raw),&f)!=nil||f.Tenant==uuid.Nil||f.Project==uuid.Nil||f.Actor==uuid.Nil||f.Session==uuid.Nil||f.Worker==uuid.Nil||f.Node==""{t.Fatal("invalid fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel();db,e:=pgx.Connect(ctx,dsn);if e!=nil{t.Fatal(e)};defer db.Close(ctx);tx,e:=db.Begin(ctx);if e!=nil{t.Fatal(e)};defer tx.Rollback(context.WithoutCancel(ctx))
 exec:=func(sql string,args ...any){t.Helper();if _,e=tx.Exec(ctx,sql,args...);e!=nil{t.Fatal(e)}}
 exec(`SELECT set_config('app.tenant_id',$1,true)`,f.Tenant.String())
 read:=func(operation string)bool{t.Helper();exec(`SET LOCAL ROLE photo_api`);var ready bool;e=tx.QueryRow(ctx,`SELECT photo.editor_capability_ready($1,$2,$3,$4,$5)`,f.Tenant,f.Project,f.Actor,f.Session,operation).Scan(&ready);exec(`RESET ROLE`);if e!=nil{t.Fatal(e)};return ready}
 if !read("editor.effects_seal")||!read("editor.layers_precompose"){t.Fatal("fixture lacks real active qualified readiness")}
 for _,tc:=range []struct{name,sql string;args []any}{
  {"disabled node",`UPDATE photo.resource_nodes SET enabled=false WHERE node_id=$1`,[]any{f.Node}},
  {"stale node",`UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '1 year' WHERE node_id=$1`,[]any{f.Node}},
  {"disabled worker",`UPDATE photo.worker_registrations SET enabled=false WHERE tenant_id=$1 AND worker_id=$2`,[]any{f.Tenant,f.Worker}},
  {"future worker",`UPDATE photo.worker_registrations SET last_seen=clock_timestamp()+interval '1 day' WHERE tenant_id=$1 AND worker_id=$2`,[]any{f.Tenant,f.Worker}},
  {"withdrawn qualification",`UPDATE photo.worker_registrations SET qualified_operations=qualified_operations-'editor.effects_seal' WHERE tenant_id=$1 AND worker_id=$2`,[]any{f.Tenant,f.Worker}},
  {"archived project",`UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`,[]any{f.Tenant,f.Project}},
 }{t.Run(tc.name,func(t *testing.T){exec(`SAVEPOINT readiness_case`);exec(tc.sql,tc.args...);if read("editor.effects_seal"){t.Error("withdrawn authority reported ready")};exec(`ROLLBACK TO SAVEPOINT readiness_case`)})}
 if read("render.compose"){t.Fatal("unallowlisted operation accepted")}
 var api,worker,public bool;e=tx.QueryRow(ctx,`SELECT has_function_privilege('photo_api','photo.editor_capability_ready(uuid,uuid,uuid,uuid,text)','EXECUTE'),has_function_privilege('photo_worker','photo.editor_capability_ready(uuid,uuid,uuid,uuid,text)','EXECUTE'),EXISTS(SELECT FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid='photo.editor_capability_ready(uuid,uuid,uuid,uuid,text)'::regprocedure AND a.grantee=0 AND a.privilege_type='EXECUTE')`).Scan(&api,&worker,&public);if e!=nil||!api||worker||public{t.Fatal("API-only ACL drift",api,worker,public,e)}
 var leaked bool;e=tx.QueryRow(ctx,`SELECT has_function_privilege('photo_api','photo.worker_authorized(uuid,uuid,text,text,text,text,text)','EXECUTE') OR has_function_privilege('photo_api','photo.editor_internal_dispatch_installed(text)','EXECUTE')`).Scan(&leaked);if e!=nil||leaked{t.Fatal("private helper exposed",leaked,e)}
}
