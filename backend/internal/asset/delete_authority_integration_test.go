package asset

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Authored real lock-wait scenarios, not executed qualification.
func TestIntegrationAssetDeleteRechecksCurrentAuthorityAfterRowWait(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated PostgreSQL required")}
 for _,scenario:=range []string{"admin_demoted","editor_acl_removed","stale_revision","current_editor"}{t.Run(scenario,func(t *testing.T){
  ctx,cancel:=context.WithTimeout(context.Background(),25*time.Second);defer cancel()
  admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
  runtimeDSN:=os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL");if runtimeDSN==""{t.Fatal("isolated nonprivileged API runtime connection required")};config,err:=pgxpool.ParseConfig(runtimeDSN);if err!=nil{t.Fatal(err)};config.ConnConfig.RuntimeParams["role"]="photo_api";database.GuardApplicationRole(config)
  api,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil{t.Fatal(err)};defer api.Close()
  scan:=func(query string,dest any,args ...any){t.Helper();if e:=admin.QueryRow(ctx,query,args...).Scan(dest);e!=nil{t.Fatal(e)}}
  exec:=func(query string,args ...any){t.Helper();if _,e:=admin.Exec(ctx,query,args...);e!=nil{t.Fatal(e)}}
  var owner,user,tid,projectID,original,id uuid.UUID;var epoch int64
  scan(`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,&owner,uuid.NewString()+"@delete.test")
  scan(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,&tid,owner)
  if err=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@delete.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
  role:="editor";if scenario=="admin_demoted"{role="admin"}
  exec(`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,$3,'active')`,tid,user,role)
  session:=uuid.New();exec(`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New())
  scan(`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Restricted','restricted') RETURNING id`,&projectID,tid)
  if scenario!="admin_demoted"{exec(`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')`,tid,projectID,user)}
  scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES($1,'photo',$2,'v1',$3,1,'image/jpeg','working_image',now()) RETURNING id`,&original,tid,uuid.NewString(),strings.Repeat("a",64))
  scan(`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,&id,tid,projectID,original)
  holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(ctx)
  var blocker int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);err!=nil{t.Fatal(err)}
  var held uuid.UUID;if err=holder.QueryRow(ctx,`SELECT id FROM photo.assets WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,id).Scan(&held);err!=nil{t.Fatal(err)}
  if scenario=="stale_revision"{if _,err=holder.Exec(ctx,`UPDATE photo.assets SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,id);err!=nil{t.Fatal(err)}}
  who:=auth.Identity{UserID:user,SessionID:session,Epoch:epoch};service:=Service{DB:api}
  type result struct{job JobAccepted;err error};done:=make(chan result,1)
  go func(){job,e:=service.Delete(ctx,who,tid,id,1,DeleteRequest{ExpectedRevision:1,ConfirmResourceID:id});done<-result{job,e}}()
  timer:=time.NewTimer(4*time.Second);defer timer.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();blocked:=false
  for !blocked{select{case r:=<-done:t.Fatal("delete did not reach asset lock",r.err);case <-timer.C:t.Fatal("asset lock wait not observed");case <-tick.C:scan(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,&blocked,blocker)}}
  if scenario=="admin_demoted"{exec(`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tid,user)}
  if scenario=="editor_acl_removed"{exec(`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,projectID,user)}
  if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
  var got result;select{case got=<-done:case <-ctx.Done():t.Fatal(ctx.Err())}
  var deleted bool;var revision int64;if err=admin.QueryRow(ctx,`SELECT deleted_at IS NOT NULL,revision FROM photo.assets WHERE tenant_id=$1 AND id=$2`,tid,id).Scan(&deleted,&revision);err!=nil{t.Fatal(err)}
  var jobs,audits int;scan(`SELECT count(*) FROM photo.deletion_jobs WHERE tenant_id=$1 AND resource_type='asset' AND resource_id=$2`,&jobs,tid,id)
  scan(`SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND action='asset.delete' AND resource_id=$2`,&audits,tid,id)
  if scenario=="current_editor"{if got.err!=nil||got.job.JobID==uuid.Nil||got.job.Status!="queued"||!deleted||revision!=2||jobs!=1||audits!=1{t.Fatal("authorized delete failed",got,deleted,revision,jobs,audits)};return}
  want:="NOT_FOUND";wantRevision:=int64(1);if scenario=="stale_revision"{want="REVISION_CONFLICT";wantRevision=2}
  if domain.AsError(got.err).Code!=want||deleted||revision!=wantRevision||jobs!=0||audits!=0{t.Fatal("refused deletion left side effects",want,got.err,deleted,revision,jobs,audits)}
 })}
}
