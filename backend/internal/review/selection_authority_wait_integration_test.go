package review

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Exercises the actual initial selection SHARE wait and project refusal. The
// deliberately incomplete canonical output is never treated as a positive
// selection fixture: authorization must refuse before canonical result lookup.
func TestIntegrationSelectedReadersRefreshAdminAfterShareWait(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated PostgreSQL required")}
 for _,kind:=range []string{"optical.negative","render.compose","portrait.glare"}{t.Run(kind,func(t *testing.T){
  ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
  admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
  cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)}
  app:="selected-wait-"+uuid.NewString();cfg.ConnConfig.RuntimeParams["role"]="photo_api";cfg.ConnConfig.RuntimeParams["application_name"]=app
  api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
  query:=func(sql string,args []any,dest ...any){t.Helper();if e:=admin.QueryRow(ctx,sql,args...).Scan(dest...);e!=nil{t.Fatal(e)}}
  exec:=func(sql string,args ...any){t.Helper();if _,e:=admin.Exec(ctx,sql,args...);e!=nil{t.Fatal(e)}}
  var owner,tid,project,snapshot,run,task,params,base,candidate,review uuid.UUID;var epoch int64;var who auth.Identity
  query(`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,[]any{uuid.NewString()+"@selected-owner.test"},&owner,&epoch)
  query(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,[]any{owner},&tid)
  query(`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,[]any{uuid.NewString()+"@selected-admin.test"},&who.UserID,&who.Epoch)
  exec(`INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'admin')`,tid,who.UserID)
  query(`INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id`,[]any{who.UserID,auth.HashSecret(uuid.NewString()),uuid.New()},&who.SessionID)
  query(`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Selection private','restricted') RETURNING id`,[]any{tid},&project)
  sha:=strings.Repeat("a",64)
  query(`INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,1) RETURNING id`,[]any{tid,who.UserID,sha},&snapshot)
  query(`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'v2','waiting_review',$2,'{}',$3,$4,true) RETURNING id`,[]any{tid,snapshot,sha,who.UserID},&run)
  exec(`INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES($1,$2,$3,$4,'preview','v2.2',$5)`,tid,project,run,snapshot,sha)
  artifact:=func(role string)uuid.UUID{t.Helper();var id uuid.UUID;query(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,2,'application/json',$4,'ready',now()) RETURNING id`,[]any{tid,uuid.NewString(),sha,role},&id);return id}
  params=artifact("task_params");base=artifact("working_image");candidate=artifact("working_image")
  query(`INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES($1,$2,'selected',$3,'1','gpu','ready','[]',$4,$5,'{}') RETURNING id`,[]any{tid,run,kind,params,sha},&task)
  query(`INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,candidate_hash,base_artifact_id,kind,state,revision,decision,decision_route,decision_hash,selection,decided_by,decided_at) VALUES($1,$2,$3,$4,$5,$6,$7,'decided',2,'accept','candidate',$5,'{}',$8,now()) RETURNING id`,[]any{tid,run,task,candidate,sha,base,kind,who.UserID},&review)
  blocker,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer blocker.Rollback(context.WithoutCancel(ctx))
  if _,err=blocker.Exec(ctx,`SELECT id FROM photo.runs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,run);err!=nil{t.Fatal(err)}
  service:=Service{DB:api};done:=make(chan error,1)
  go func(){var e error;switch kind{case "optical.negative":_,e=service.ReadNegativeResultSelection(ctx,who,tid,review);case "render.compose":_,e=service.ReadRenderResultSelection(ctx,who,tid,review);default:_,e=service.ReadRestorationResultSelection(ctx,who,tid,review)};done<-e}()
  deadline:=time.Now().Add(3*time.Second)
  for{var waiting bool;query(`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE '%FOR SHARE OF%')`,[]any{app},&waiting);if waiting{break};select{case e:=<-done:t.Fatal("did not reach selection share wait",e);default:};if time.Now().After(deadline){t.Fatal("share wait not observed")};time.Sleep(10*time.Millisecond)}
  exec(`UPDATE photo.memberships SET role='editor',revision=revision+1 WHERE tenant_id=$1 AND user_id=$2`,tid,who.UserID)
  if err=blocker.Commit(ctx);err!=nil{t.Fatal(err)}
  if err=<-done;err==nil||domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("demoted admin bypassed private project",err)}
 })}
}
