package v2render
import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Authored isolation scenario; not a positive physically admitted multi run.
func TestIntegrationMultiHistoryScopeACLAndAbsentAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.read_v29_multi_run_review_scope(uuid,uuid,uuid,uuid)"
 var secure,owned,public bool
 err=db.QueryRow(ctx,`SELECT p.prosecdef,r.rolname='photo_fn_owner',EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&secure,&owned,&public)
 if err!=nil||!secure||!owned||public{t.Fatal("unsafe scope reader",err)}
 for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal","photo_auth","photo_publisher","photo_maintenance"}{var allowed bool;if err=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,signature).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed!=(role=="photo_api"){t.Fatal("wrong ACL",role)}}
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
 current:=uuid.New();if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",current.String());err!=nil{t.Fatal(err)}
 for _,tenantID:=range []uuid.UUID{current,uuid.New()}{var raw []byte;if err=tx.QueryRow(ctx,"SELECT photo.read_v29_multi_run_review_scope($1,$2,$3,$4)",tenantID,uuid.New(),uuid.New(),uuid.New()).Scan(&raw);err!=nil{t.Fatal(err)};if len(raw)!=0{t.Fatal("absent/foreign scope disclosed")}}
}
