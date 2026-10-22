package review
import("context";"os";"strings";"testing";"time";"github.com/google/uuid";"github.com/jackc/pgx/v5")
func TestIntegrationLocalPortraitDiscoveryCurrentAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database through02487 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel();db,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close(context.WithoutCancel(ctx))
 var definition string;var api,worker,public bool
 err=db.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),has_function_privilege('photo_api',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') FROM pg_proc p WHERE p.oid='photo.discover_v28_local_portrait_result(uuid,uuid,uuid,uuid)'::regprocedure`).Scan(&definition,&api,&worker,&public)
 if err!=nil{t.Fatal(err)};if !api||worker||public{t.Fatal("discovery ACL drift")}
 for _,guard:=range []string{"v2_editor_authorized","get_v28_local_portrait_result","selected.run_id=r.id","r.status='completed'","NOT r.cancel_requested","review_id","portrait.hair","portrait.clothing"}{if !strings.Contains(definition,guard){t.Fatal("missing current-authority guard",guard)}}
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx));if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 tenant:=uuid.New();if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{t.Fatal(err)}
 var run uuid.UUID;var workflow *uuid.UUID
 err=tx.QueryRow(ctx,`SELECT run_id,workflow_id FROM photo.discover_v28_local_portrait_result($1,$2,$3,$4)`,tenant,uuid.New(),uuid.New(),uuid.New()).Scan(&run,&workflow)
 if err!=pgx.ErrNoRows{t.Fatal("unknown actor/source disclosed result",err)}
}
