package style

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
)

type styleBarrierSigner struct { afterSign func() }
func (s styleBarrierSigner)SignDownload(_ context.Context,o objectstore.Object,_ string,_ time.Duration)(objectstore.ReadURL,error){if s.afterSign!=nil{s.afterSign()};return objectstore.ReadURL{URL:"https://objects.example/style?versionId="+o.VersionID,ExpiresAt:time.Now().Add(time.Minute)},nil}

// Relational source scenarios, not executed verification or artifact pixel proof.
func TestIntegrationStyleUpdateAndExportCurrentAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated PostgreSQL required")}
 for _,scenario:=range []string{"export_current","export_acl_removed","export_artifact_withdrawn","export_cancelled","update_lock_admin_demoted"}{t.Run(scenario,func(t *testing.T){
  ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
  admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close();cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
  api,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer api.Close()
  exec:=func(q string,args ...any){t.Helper();if _,err:=admin.Exec(ctx,q,args...);err!=nil{t.Fatal(err)}}
  var owner,actor,tid,pid,sid,artifact,version uuid.UUID;var epoch int64
  if e=admin.QueryRow(ctx,`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&owner);e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,owner).Scan(&tid);e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&actor,&epoch);e!=nil{t.Fatal(e)}
  role:="editor";if scenario=="update_lock_admin_demoted"{role="admin"};exec(`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,$3,'active')`,tid,actor,role)
  session:=uuid.New();exec(`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,actor,auth.HashSecret(uuid.NewString()),uuid.New())
  if e=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Style authority','restricted') RETURNING id`,tid).Scan(&pid);e!=nil{t.Fatal(e)}
  if scenario!="update_lock_admin_demoted"{exec(`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')`,tid,pid,actor)}
  if e=admin.QueryRow(ctx,`INSERT INTO photo.styles(tenant_id,project_id,name,description,created_by) VALUES($1,$2,'Original','',$3) RETURNING id`,tid,pid,actor).Scan(&sid);e!=nil{t.Fatal(e)}
  who:=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch};svc:=Service{DB:api,Bucket:"photo"}
  if scenario=="update_lock_admin_demoted"{
   holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(ctx);var blocker int;var held uuid.UUID
   if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);err!=nil{t.Fatal(err)};if err=holder.QueryRow(ctx,`SELECT id FROM photo.styles WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,sid).Scan(&held);err!=nil{t.Fatal(err)}
   done:=make(chan error,1);name:="Must not commit";go func(){_,err:=svc.Update(ctx,who,tid,sid,1,Patch{ExpectedRevision:1,Name:&name});done<-err}()
   deadline:=time.NewTimer(3*time.Second);defer deadline.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();waiting:=false
   for !waiting{select{case err:=<-done:t.Fatal("style wait not reached",err);case <-deadline.C:t.Fatal("style wait not observed");case <-tick.C:if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blocker).Scan(&waiting);err!=nil{t.Fatal(err)}}}
   exec(`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tid,actor);if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
   select{case err=<-done:if domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("stale admin bypass",err)};case <-ctx.Done():t.Fatal(ctx.Err())}
   var nameNow string;var rev int64;if e=admin.QueryRow(ctx,`SELECT name,revision FROM photo.styles WHERE tenant_id=$1 AND id=$2`,tid,sid).Scan(&nameNow,&rev);e!=nil||nameNow!="Original"||rev!=1{t.Fatal("refused style mutation committed",nameNow,rev,e)};return
  }
  sha,e:=canonical.Hash(json.RawMessage(`{}`));if e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES($1,'photo',$2,'v1',$3,2,'application/json','ready',now()) RETURNING id`,tid,uuid.NewString(),sha).Scan(&artifact);e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES($1,$2,1,'{}',$3,'2.0.0','photo_renderer_v2','accepted','{}',$4) RETURNING id`,tid,sid,sha,artifact).Scan(&version);e!=nil{t.Fatal(e)}
  svc.Objects=styleBarrierSigner{afterSign:func(){switch scenario{case "export_acl_removed":exec(`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,pid,actor);case "export_artifact_withdrawn":exec(`UPDATE photo.artifacts SET state='deleting' WHERE tenant_id=$1 AND id=$2`,tid,artifact);case "export_cancelled":cancel()}}}
  out,err:=svc.Export(ctx,who,tid,sid,version);if scenario=="export_current"{if err!=nil||out.URL==""{t.Fatal("current export refused",out,err)};return}
  if err==nil||out.URL!=""{t.Fatal("late export bearer escaped",out,err)}
 })}
}
