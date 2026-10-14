package v2optical
import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Installation/isolation only. Full source/lease/adoption fixtures remain separate.
func TestIntegrationMultiExecutionSessionPrivateAndAbsentScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.v29_multi_execution_session(uuid,uuid,jsonb)"
 var secure,owned,public bool
 if err=db.QueryRow(ctx,`SELECT p.prosecdef,r.rolname='photo_fn_owner',EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&secure,&owned,&public);err!=nil{t.Fatal(err)}
 if !secure||!owned||public{t.Fatal("unsafe resolver")}
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_portal"}{var allowed bool;if err=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,signature).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed{t.Fatal("private resolver exposed",role)}}
 for _,fn:=range []string{
 "photo.v29_worker_multi_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)",
 "photo.v29_worker_multi_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)",
 "photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)",
 }{var routed bool;if err=db.QueryRow(ctx,"SELECT strpos(pg_get_functiondef(to_regprocedure($1)),'photo.v29_multi_execution_session(p_tenant, r.id, r.config_snapshot)')>0 OR strpos(pg_get_functiondef(to_regprocedure($1)),'photo.v29_multi_execution_session(p_tenant,r.id,r.config_snapshot)')>0",fn).Scan(&routed);err!=nil{t.Fatal(err)};if !routed{t.Fatal("session routing absent",fn)}}
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 tenant:=uuid.New();if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{t.Fatal(err)}
 for _,scope:=range []uuid.UUID{tenant,uuid.New()}{var session *uuid.UUID;if err=tx.QueryRow(ctx,"SELECT photo.v29_multi_execution_session($1,$2,'{}'::jsonb)",scope,uuid.New()).Scan(&session);err!=nil{t.Fatal(err)};if session!=nil{t.Fatal("absent/foreign execution accepted")}}
}
