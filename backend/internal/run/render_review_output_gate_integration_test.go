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

func TestIntegrationRenderReviewDerivativeOutputGate(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01792 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)}
 defer conn.Close(context.WithoutCancel(ctx))
 for _,tc:=range []struct{signature string;api bool;needles []string}{
  {"photo.v26_render_derivative_outputs_before_review_gate(uuid,uuid,uuid,uuid)",false,[]string{"v26_render_derivative_source(","v26_derivative_receipt"}},
  {"photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid)",true,[]string{"v26_render_derivative_outputs_before_review_gate(","v26_render_review_completion_ready(","v.state='decided'","IS NOT TRUE"}},
 }{
  var owner,definition string
  var secdef,api,worker,scheduler,public bool
  err=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),
   has_function_privilege('photo_api',p.oid,'EXECUTE'),
   has_function_privilege('photo_worker',p.oid,'EXECUTE'),
   has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),
   EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
   FROM pg_proc p WHERE p.oid=to_regprocedure($1)`,tc.signature).
   Scan(&owner,&secdef,&definition,&api,&worker,&scheduler,&public)
  if err!=nil{t.Fatal(err)}
  if owner!="photo_fn_owner"||!secdef||api!=tc.api||worker||scheduler||public{t.Fatal("render derivative output ACL drift",tc.signature)}
  for _,needle:=range tc.needles{if !strings.Contains(definition,needle){t.Fatal("render derivative output gate drift",tc.signature,needle)}}
 }
 tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
 tenant,actor,session,run:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{t.Fatal(err)}
 rows,err:=tx.Query(ctx,"SELECT artifact_id FROM photo.v26_render_derivative_outputs($1,$2,$3,$4)",actor,session,tenant,run)
 if err!=nil{t.Fatal(err)}
 if rows.Next(){rows.Close();t.Fatal("unknown principal received render derivative")}
 err=rows.Err();rows.Close();if err!=nil{t.Fatal(err)}
}
