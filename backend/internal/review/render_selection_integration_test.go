package review

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

func TestIntegrationRenderSelectionAuthorityInstalled(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01789 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 for _,tc:=range []struct{signature string;api,scheduler bool;needles []string}{
  {"photo.v26_render_review_selection(uuid,uuid)",false,true,[]string{"v.decision_route='base'","root.output_role='document'","c.output_role='document'","v2-adopt"}},
  {"photo.get_v26_render_review_selection(uuid,uuid,uuid,uuid)",true,false,[]string{"authorize_tenant(","project_acl","v26_render_review_selection("}},
 }{
  var owner,definition,configuration string
  var secdef,api,scheduler,worker,pub bool
  err=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),
 coalesce(array_to_string(p.proconfig,','),''),has_function_privilege('photo_api',p.oid,'EXECUTE'),
 has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),
 EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
 WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure($1)`,tc.signature).
 Scan(&owner,&secdef,&definition,&configuration,&api,&scheduler,&worker,&pub)
  if err!=nil{t.Fatal(err)}
  if owner!="photo_fn_owner"||!secdef||api!=tc.api||scheduler!=tc.scheduler||worker||pub||!strings.Contains(strings.ReplaceAll(configuration," ",""),"search_path=pg_catalog,photo"){
   t.Fatal("render selection SQL authority drift",tc.signature)
  }
  for _,needle:=range tc.needles{if !strings.Contains(definition,needle){t.Fatal("render selection SQL definition drift",tc.signature,needle)}}
 }
 tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
 tid,actor,session,reviewID:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tid.String());err!=nil{t.Fatal(err)}
 rows,err:=tx.Query(ctx,"SELECT review_id FROM photo.get_v26_render_review_selection($1,$2,$3,$4)",tid,actor,session,reviewID)
 if err!=nil{t.Fatal(err)}
 if rows.Next(){rows.Close();t.Fatal("unknown session read render selection")}
 err=rows.Err();rows.Close();if err!=nil{t.Fatal(err)}
}
