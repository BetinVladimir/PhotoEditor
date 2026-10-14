package run

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

func TestIntegrationRenderPreviewOutputPrincipalGate(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01793 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)}
 defer conn.Close(context.WithoutCancel(ctx))
 var owner,definition string
 var secdef,api,scheduler,worker,public bool
 err=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),
 has_function_privilege('photo_api',p.oid,'EXECUTE'),
 has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),
 has_function_privilege('photo_worker',p.oid,'EXECUTE'),
 EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
 WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure('photo.v26_render_preview_output_allowed(uuid,uuid,uuid,uuid)')`).
 Scan(&owner,&secdef,&definition,&api,&scheduler,&worker,&public)
 if err!=nil{t.Fatal(err)}
 if owner!="photo_fn_owner"||!secdef||!api||scheduler||worker||public{t.Fatal("render preview output principal gate ACL drift")}
 for _,needle:=range []string{"authorize_tenant(","project_acl","v26_render_review_completion_ready(","IS NOT TRUE"}{
  if !strings.Contains(definition,needle){t.Fatal("render preview output principal gate drift",needle)}
 }
 tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
 tenant,actor,session,preview:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{t.Fatal(err)}
 var allowed bool
 if err=tx.QueryRow(ctx,"SELECT photo.v26_render_preview_output_allowed($1,$2,$3,$4)",actor,session,tenant,preview).Scan(&allowed);err!=nil{t.Fatal(err)}
 if allowed{t.Fatal("unknown session authorized preview output")}
}
