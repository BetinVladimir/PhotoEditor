package review
import("context";"os";"strings";"testing";"time";"github.com/google/uuid";"github.com/jackc/pgx/v5")
func TestIntegrationGlareSelectedApplyAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database through02479 required")};ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 var definition string;var api,worker,scheduler,public bool
 err=conn.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),has_function_privilege('photo_api',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE') FROM pg_proc p WHERE p.oid='photo.apply_v28_glare_version(uuid,uuid,uuid,uuid,text,text,text)'::regprocedure`).Scan(&definition,&api,&worker,&scheduler,&public)
 if err!=nil{t.Fatal(err)};if !api||worker||scheduler||public{t.Fatal("glare apply ACL drift")}
 for _,needle:=range []string{"v28_restoration_descriptor_jcs","get_v28_restoration_review_selection","v28_glare_selected_authority","V28.GlareAppliedVersion","glare_source","glare_donor","body->>'mode'='generative'","v2_artifact_links"}{if !strings.Contains(definition,needle){t.Fatal("glare selected persistence guard missing",needle)}}
 if strings.Contains(definition,"open_eyes")||strings.Contains(definition,"v28_restoration_lease_ready"){t.Fatal("foreign lease or donor authority reused")}
 tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx));if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 tenant:=uuid.New();if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{t.Fatal(err)}
 var version *uuid.UUID;if err=tx.QueryRow(ctx,`SELECT photo.apply_v28_glare_version($1,$2,$3,$4,$5,$6,$7)`,tenant,uuid.New(),uuid.New(),uuid.New(),strings.Repeat("a",64),`{}`,`{}`).Scan(&version);err!=nil{t.Fatal(err)};if version!=nil{t.Fatal("unknown actor/source created version")}
}
