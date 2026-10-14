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

func TestIntegrationRenderReviewCompletionReadinessAuthority(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn==""{t.Skip("isolated database migrated through 01790 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)}
 defer conn.Close(context.WithoutCancel(ctx))
 var owner,definition string
 var secdef,scheduler,api,worker,public bool
 err=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),
 has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),
 has_function_privilege('photo_api',p.oid,'EXECUTE'),
 has_function_privilege('photo_worker',p.oid,'EXECUTE'),
 EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
  WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure('photo.v26_render_review_completion_ready(uuid,uuid)')`).
  Scan(&owner,&secdef,&definition,&scheduler,&api,&worker,&public)
 if err!=nil{t.Fatal(err)}
 if owner!="photo_fn_owner"||!secdef||!scheduler||api||worker||public{t.Fatal("render review completion authority drift")}
 for _,needle:=range []string{"v26_render_review_selection(","task_edges","v2_task_inputs","v26_derivative_task_id","selected.route IN"}{
  if !strings.Contains(definition,needle){t.Fatal("render completion readiness definition drift",needle)}
 }
 tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_scheduler");err!=nil{t.Fatal(err)}
 var ready bool
 if err=tx.QueryRow(ctx,"SELECT photo.v26_render_review_completion_ready($1,$2)",uuid.New(),uuid.New()).Scan(&ready);err!=nil{t.Fatal(err)}
 if ready{t.Fatal("unknown render task marked completion-ready")}
}
