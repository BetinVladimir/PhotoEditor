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

func TestIntegrationCandidateReviewCanonicalOutputGateAuthority(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through 01795 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 var owner,definition,configuration string
 var secdef,api,scheduler,worker,pub bool
 err=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),
 coalesce(array_to_string(p.proconfig,','),''),has_function_privilege('photo_api',p.oid,'EXECUTE'),
 has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),
 EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
 WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure('photo.candidate_review_canonical_output_allowed(uuid,uuid,uuid,uuid)')`).
 Scan(&owner,&secdef,&definition,&configuration,&api,&scheduler,&worker,&pub)
 if err!=nil{t.Fatal(err)}
 if owner!="photo_fn_owner"||!secdef||!api||scheduler||worker||pub||!strings.Contains(strings.ReplaceAll(configuration," ",""),"search_path=pg_catalog,photo") {t.Fatal("candidate output gate authority drift")}
 for _,needle:=range []string{"authorize_tenant(","render.compose","optical.negative","result.review_required IS TRUE","result.review_required IS FALSE","v26_render_review_completion_ready(","v29_negative_review_completion_ready("}{if !strings.Contains(definition,needle){t.Fatal("candidate output gate definition drift",needle)}}
 tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
 tenant,actor,session,task:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{t.Fatal(err)}
 var allowed bool
 if err=tx.QueryRow(ctx,"SELECT photo.candidate_review_canonical_output_allowed($1,$2,$3,$4)",actor,session,tenant,task).Scan(&allowed);err!=nil{t.Fatal(err)}
 if allowed{t.Fatal("unknown session/task allowed canonical output")}
 if err=tx.Rollback(ctx);err!=nil{t.Fatal(err)}

 // A canonical result that requires review must stay private even before a
 // review row has been created. Keep the fixture inside one rolled-back tx.
 fixture,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)};defer fixture.Rollback(context.WithoutCancel(ctx))
 var user,tid,snapshot,runID,params,renderTask,result uuid.UUID
 var epoch int64
 if err=fixture.QueryRow(ctx,"SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@test.example").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
 if err=fixture.QueryRow(ctx,"SELECT id FROM photo.tenants WHERE owner_user_id=$1",user).Scan(&tid);err!=nil{t.Fatal(err)}
 sid:=uuid.New();digest:=strings.Repeat("a",64)
 if _,err=fixture.Exec(ctx,"INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')",sid,user,digest,uuid.New());err!=nil{t.Fatal(err)}
 if err=fixture.QueryRow(ctx,"INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,1) RETURNING id",tid,user,digest).Scan(&snapshot);err!=nil{t.Fatal(err)}
 if err=fixture.QueryRow(ctx,"INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'v2',$2,'{}',$3,$4) RETURNING id",tid,snapshot,digest,user).Scan(&runID);err!=nil{t.Fatal(err)}
 if err=fixture.QueryRow(ctx,"INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id",tid,uuid.NewString(),digest).Scan(&params);err!=nil{t.Fatal(err)}
 if err=fixture.QueryRow(ctx,"INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'render.compose:0','render.compose','1','gpu','ready','[]',$3,$4,1,1,'{}') RETURNING id",tid,runID,params,digest).Scan(&renderTask);err!=nil{t.Fatal(err)}
 if err=fixture.QueryRow(ctx,"INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest,review_required) VALUES($1,$2,1,1,'[]','{}',$3,true) RETURNING id",tid,renderTask,digest).Scan(&result);err!=nil{t.Fatal(err)}
 if _,err=fixture.Exec(ctx,"UPDATE photo.tasks SET canonical_result_id=$1 WHERE id=$2",result,renderTask);err!=nil{t.Fatal(err)}
 if _,err=fixture.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tid.String());err!=nil{t.Fatal(err)}
 if _,err=fixture.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
 if err=fixture.QueryRow(ctx,"SELECT photo.candidate_review_canonical_output_allowed($1,$2,$3,$4)",user,sid,tid,renderTask).Scan(&allowed);err!=nil{t.Fatal(err)}
 if allowed{t.Fatal("review-required task without review exposed candidate")}
}
