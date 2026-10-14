package workflow

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

func TestIntegrationRenderSelectedReviewFinalizerAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01791 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)}
 defer conn.Close(context.WithoutCancel(ctx))
 for _,tc:=range []struct{signature string;scheduler bool;needles []string}{
  {"photo.v26_render_selected_run_ready(uuid,uuid)",false,[]string{"v26_render_review_completion_ready(","v26_render_review_selection(","v2-adopt","graph_expansions"}},
  {"photo.finalize_v26_render_selected_run()",true,[]string{"v26_render_selected_run_ready(","FOR UPDATE SKIP LOCKED","FOR SHARE","v2.run.completed"}},
 }{
  var owner,definition string
  var secdef,scheduler,api,worker,public bool
  err=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),
   has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),
   has_function_privilege('photo_api',p.oid,'EXECUTE'),
   has_function_privilege('photo_worker',p.oid,'EXECUTE'),
   EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
   FROM pg_proc p WHERE p.oid=to_regprocedure($1)`,tc.signature).
   Scan(&owner,&secdef,&definition,&scheduler,&api,&worker,&public)
  if err!=nil{t.Fatal(err)}
  if owner!="photo_fn_owner"||!secdef||scheduler!=tc.scheduler||api||worker||public{t.Fatal("render selected finalizer privilege drift",tc.signature)}
  for _,needle:=range tc.needles{if !strings.Contains(definition,needle){t.Fatal("render selected finalizer definition drift",tc.signature,needle)}}
 }
 var ready bool
 if err=conn.QueryRow(ctx,"SELECT photo.v26_render_selected_run_ready($1,$2)",uuid.New(),uuid.New()).Scan(&ready);err!=nil{t.Fatal(err)}
 if ready{t.Fatal("unknown run marked render completion-ready")}
}
