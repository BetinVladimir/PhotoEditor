package workflow

import("context";"os";"strings";"testing";"time";"github.com/google/uuid";"github.com/jackc/pgx/v5")

// Authored for the later isolated database pass; does not install migrations.
func TestIntegrationEditorFinalAuthorityAndUnknownOrigin(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through02474 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 for _,signature:=range []string{"photo.begin_editor_effect_final_render(uuid,uuid,uuid,uuid,text,text,bigint,jsonb)","photo.activate_editor_effect_final_render(uuid,uuid,uuid)","photo.settle_editor_effect_final_render()","photo.withdraw_editor_effect_build()"}{
  var owner string;var secdef,api,worker,scheduler,public bool
  err=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,
   has_function_privilege('photo_api',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),
   EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
   FROM pg_proc p WHERE p.oid=to_regprocedure($1)`,signature).Scan(&owner,&secdef,&api,&worker,&scheduler,&public)
  if err!=nil{t.Fatal(err)};if owner!="photo_fn_owner"||!secdef||api||worker||!scheduler||public{t.Fatal("private Editor lifecycle authority drift",signature)}
 }
 var definition string
 if err=conn.QueryRow(ctx,`SELECT pg_get_functiondef('photo.editor_effect_render_receipt_canonical(uuid,uuid,uuid)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 for _,needle:=range []string{"photo.v2_task_inputs","EditorExecutedBaseReceipt","ImageResult","typed.sha256=a.sha256","typed.sha256=root.sha256","a.produced_attempt=result.attempt","root.produced_fence=result.fencing_token"}{if !strings.Contains(definition,needle){t.Fatal("canonical receipt edge missing",needle)}}
 if strings.Contains(definition,"t.inputs @>"){t.Fatal("transitive receipt incorrectly requires direct root membership")}
 tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_scheduler`);err!=nil{t.Fatal(err)}
 tenant,build:=uuid.New(),uuid.New();var activated bool
 if err=tx.QueryRow(ctx,`SELECT photo.activate_editor_effect_final_render($1,$2,$3)`,tenant,build,uuid.New()).Scan(&activated);err!=nil{t.Fatal(err)};if activated{t.Fatal("unknown origin activated")}
 rows,err:=tx.Query(ctx,`SELECT * FROM photo.editor_effect_final_artifact($1,$2,$3,$4)`,tenant,build,uuid.New(),strings.Repeat("a",64));if err!=nil{t.Fatal(err)}
 if rows.Next(){rows.Close();t.Fatal("unknown origin exposed tenant artifact")};err=rows.Err();rows.Close();if err!=nil{t.Fatal(err)}
}
