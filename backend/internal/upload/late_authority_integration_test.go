package upload

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
)

type signingBarrierStore struct { multipartFake; afterSign func() }
func (s *signingBarrierStore) SignUploadParts(ctx context.Context,key,token string,parts []int,ttl time.Duration)([]objectstore.PartURL,error){
 result,err:=s.multipartFake.SignUploadParts(ctx,key,token,parts,ttl)
 if s.afterSign!=nil{s.afterSign()};return result,err
}

// Actual API-role post-I/O authorization cases; authored only.
func TestIntegrationUploadDoesNotDiscloseLateSignedParts(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 for _,scenario:=range []string{"restricted_acl_withdrawn","upload_aborted","context_cancelled","abort_quota_wait_acl_withdrawn","begin_quota_wait_admin_demoted"}{t.Run(scenario,func(t *testing.T){
  ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
  admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close()
  cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
  api,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer api.Close()
  var owner,actor,tid,pid uuid.UUID;var epoch int64
  if e=admin.QueryRow(ctx,`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&owner);e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,owner).Scan(&tid);e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&actor,&epoch);e!=nil{t.Fatal(e)}
  memberRole:="editor";if scenario=="begin_quota_wait_admin_demoted"{memberRole="admin"}
  if _,e=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,$3,'active')`,tid,actor,memberRole);e!=nil{t.Fatal(e)}
  sid:=uuid.New();if _,e=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,sid,actor,auth.HashSecret(uuid.NewString()),uuid.New());e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Upload late authority','restricted') RETURNING id`,tid).Scan(&pid);e!=nil{t.Fatal(e)}
  if scenario!="begin_quota_wait_admin_demoted"{if _,e=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')`,tid,pid,actor);e!=nil{t.Fatal(e)}}
  objects:=new(signingBarrierStore);svc:=Service{DB:api,Objects:objects};who:=auth.Identity{UserID:actor,SessionID:sid,Epoch:epoch}
  if scenario=="begin_quota_wait_admin_demoted"{
   holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(ctx)
   var blocker int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);err!=nil{t.Fatal(err)}
   var held uuid.UUID;if err=holder.QueryRow(ctx,`SELECT tenant_id FROM photo.tenant_usage WHERE tenant_id=$1 FOR UPDATE`,tid).Scan(&held);err!=nil{t.Fatal(err)}
   done:=make(chan error,1);go func(){_,err:=svc.Begin(ctx,who,tid,uuid.NewString(),BeginRequest{ProjectID:pid,Filename:"frame.jpg",Bytes:1024,Format:"jpeg"});done<-err}()
   deadline:=time.NewTimer(3*time.Second);defer deadline.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();waiting:=false
   for !waiting{select{case err:=<-done:t.Fatal("begin did not reach usage wait",err);case <-deadline.C:t.Fatal("usage wait not observed");case <-tick.C:if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blocker).Scan(&waiting);err!=nil{t.Fatal(err)}}}
   if _,err=admin.Exec(ctx,`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tid,actor);err!=nil{t.Fatal(err)}
   if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
   select{case err=<-done:if domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("post-usage role refusal",err)};case <-ctx.Done():t.Fatal(ctx.Err())}
   var sessions int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.upload_sessions WHERE tenant_id=$1`,tid).Scan(&sessions);err!=nil||sessions!=0{t.Fatal("unauthorized admission committed",sessions,err)}
   if objects.begins!=0{t.Fatal("unauthorized admission created multipart")};return
  }
  admitted,e:=svc.Begin(ctx,who,tid,uuid.NewString(),BeginRequest{ProjectID:pid,Filename:"frame.jpg",Bytes:1024,Format:"jpeg"});if e!=nil{t.Fatal(e)}
  if scenario=="abort_quota_wait_acl_withdrawn"{
   holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(ctx)
   var blocker int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);err!=nil{t.Fatal(err)}
   var held uuid.UUID;if err=holder.QueryRow(ctx,`SELECT id FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='upload' AND holder_id=$2 FOR UPDATE`,tid,admitted.ID).Scan(&held);err!=nil{t.Fatal(err)}
   done:=make(chan error,1);go func(){_,err:=svc.Abort(ctx,who,tid,admitted.ID);done<-err}()
   deadline:=time.NewTimer(3*time.Second);defer deadline.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();waiting:=false
   for !waiting{select{case err:=<-done:t.Fatal("abort did not reach quota wait",err);case <-deadline.C:t.Fatal("quota wait not observed");case <-tick.C:if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blocker).Scan(&waiting);err!=nil{t.Fatal(err)}}}
   if _,err=admin.Exec(ctx,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,pid,actor);err!=nil{t.Fatal(err)}
   if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
   select{case err=<-done:if domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("post-quota ACL refusal",err)};case <-ctx.Done():t.Fatal(ctx.Err())}
   var state string;var reserved int64
   if err=admin.QueryRow(ctx,`SELECT state FROM photo.upload_sessions WHERE tenant_id=$1 AND id=$2`,tid,admitted.ID).Scan(&state);err!=nil||state!="uploading"{t.Fatal("refused abort changed session",state,err)}
   if err=admin.QueryRow(ctx,`SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1`,tid).Scan(&reserved);err!=nil||reserved!=1024{t.Fatal("refused abort released quota",reserved,err)}
   if objects.aborts!=0{t.Fatal("refused abort mutated object storage")};return
  }
  signCtx,stop:=context.WithCancel(ctx);defer stop()
  objects.afterSign=func(){switch scenario{
   case "restricted_acl_withdrawn":_,e=admin.Exec(ctx,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,pid,actor)
   case "upload_aborted":_,e=svc.Abort(ctx,who,tid,admitted.ID)
   case "context_cancelled":stop()
  };if e!=nil{t.Fatal(e)}}
  out,err:=svc.SignParts(signCtx,who,tid,admitted.ID,PartRequest{PartNumbers:[]int{1}})
  if err==nil||len(out.Items)!=0{t.Fatal("late signed bearer escaped",out,err)}
  if scenario=="restricted_acl_withdrawn"&&domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("wrong current ACL refusal",err)}
  if scenario=="upload_aborted"&&domain.AsError(err).Code!="UPLOAD_STATE_INVALID"{t.Fatal("wrong aborted tuple refusal",err)}
 })}
}
