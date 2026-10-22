package asset

import (
 "context"
 "errors"
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
func TestVersionNavigationRefusesInvalidScopeAndPrecancelBeforeDatabase(t *testing.T){
 service:=Service{};who:=auth.Identity{UserID:uuid.New()};id:=uuid.New()
 for _,scope:=range [][3]uuid.UUID{{uuid.Nil,id,id},{id,uuid.Nil,id},{id,id,uuid.Nil}}{
  _,err:=service.GetVersionNavigation(context.Background(),who,scope[0],scope[1],scope[2]);if domain.AsError(err).Code!="VALIDATION_FAILED"{t.Fatal(err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel();if _,err:=service.GetVersionNavigation(ctx,who,id,id,id);!errors.Is(err,context.Canceled){t.Fatal(err)}
}
// Metadata-only fixtures exercise the actual service/RLS/project gate, not
// physical publication or image quality. This scenario is authored, not run.
func TestIntegrationVersionNavigationHistoricalViewerAndRefusals(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 runtimeDSN:=os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL");if runtimeDSN==""{t.Fatal("isolated nonprivileged API runtime connection required")};cfg,err:=pgxpool.ParseConfig(runtimeDSN);if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_api";database.GuardApplicationRole(cfg);application:="navigation-scope-"+uuid.NewString();cfg.ConnConfig.RuntimeParams["application_name"]=application
 api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
 scan:=func(q string,dest any,args ...any){t.Helper();if e:=admin.QueryRow(ctx,q,args...).Scan(dest);e!=nil{t.Fatal(e)}}
 exec:=func(q string,args ...any){t.Helper();if _,e:=admin.Exec(ctx,q,args...);e!=nil{t.Fatal(e)}}
 var owner,viewer,tid,foreignTenant,pid,otherProject,artifact,asset,historical,latest uuid.UUID;var epoch int64
 scan("SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)",&owner,uuid.NewString()+"@navigation.test")
 if err=admin.QueryRow(ctx,"SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@navigation.test").Scan(&viewer,&epoch);err!=nil{t.Fatal(err)}
 scan("SELECT id FROM photo.tenants WHERE owner_user_id=$1",&tid,owner);scan("SELECT id FROM photo.tenants WHERE owner_user_id=$1",&foreignTenant,viewer)
 session:=uuid.New();exec("INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')",session,viewer,auth.HashSecret(uuid.NewString()),uuid.New())
 exec("INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'viewer','active')",tid,viewer)
 scan("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Navigation','restricted') RETURNING id",&pid,tid)
 scan("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Other','workspace') RETURNING id",&otherProject,tid)
 exec("INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')",tid,pid,viewer)
 hash:=strings.Repeat("a",64)
 scan("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES($1,'test',$2,'v1',$3,1,'image/jpeg','working_image',now()) RETURNING id",&artifact,tid,uuid.NewString(),hash)
 scan("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id",&asset,tid,pid,artifact)
 scan("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,version_no) VALUES($1,$2,$3,$4,'{}','accepted',1) RETURNING id",&historical,tid,asset,artifact,hash)
 scan("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,version_no) VALUES($1,$2,$3,$4,'{}','accepted',2) RETURNING id",&latest,tid,asset,artifact,hash)
 service:=Service{DB:api};who:=auth.Identity{UserID:viewer,SessionID:session,Epoch:epoch}
 got,err:=service.GetVersionNavigation(ctx,who,tid,pid,historical);if err!=nil||got.AssetID!=asset||got.AssetVersionID!=historical||got.AssetVersionID==latest||got.ProjectID!=pid||got.TenantID!=tid{t.Fatal(got,err)}
 refuse:=func(tenant,project,version uuid.UUID){t.Helper();got,e:=service.GetVersionNavigation(ctx,who,tenant,project,version);if e==nil||got!=(VersionNavigation{}){t.Fatal("unexpected projection",got,e)}}
 // Wait inside the actual version projection, withdraw its previously granted
 // restricted-project ACL, then release. A cached entry-time project grant
 // must not publish the resolved historical asset after the wait.
 lock,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)}
 if _,err=lock.Exec(ctx,`LOCK TABLE photo.asset_versions IN ACCESS EXCLUSIVE MODE`);err!=nil{_ = lock.Rollback(ctx);t.Fatal(err)}
 type outcome struct{value VersionNavigation;err error};done:=make(chan outcome,1)
 go func(){v,e:=service.GetVersionNavigation(ctx,who,tid,pid,historical);done<-outcome{v,e}}()
 blocked:=false;deadline:=time.NewTimer(3*time.Second);ticker:=time.NewTicker(10*time.Millisecond)
 waiting:for{select{case<-deadline.C:break waiting;case<-ticker.C:if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%FROM photo.asset_versions%')`,application).Scan(&blocked);err!=nil{break waiting};if blocked{break waiting}}};ticker.Stop();deadline.Stop()
 if !blocked||err!=nil{_ = lock.Rollback(ctx);t.Fatal("projection did not reach its bounded relation wait",err)}
 exec("DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3",tid,pid,viewer)
 if err=lock.Commit(ctx);err!=nil{t.Fatal(err)}
 select{case result:=<-done:if result.err==nil||result.value!=(VersionNavigation{}){t.Fatal("late navigation exposed withdrawn project",result.value,result.err)};case<-time.After(6*time.Second):t.Fatal("bounded navigation did not settle")}
 exec("INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')",tid,pid,viewer)

 refuse(tid,otherProject,historical);refuse(foreignTenant,pid,historical);refuse(tid,pid,uuid.New())
 exec("DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3",tid,pid,viewer);refuse(tid,pid,historical)
 exec("INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')",tid,pid,viewer)
 exec("UPDATE photo.artifacts SET verified_at=NULL WHERE id=$1",artifact);refuse(tid,pid,historical)
 exec("UPDATE photo.artifacts SET verified_at=clock_timestamp() WHERE id=$1",artifact)
 exec("UPDATE photo.artifacts SET state='deleting' WHERE id=$1",artifact);refuse(tid,pid,historical)
 exec("UPDATE photo.artifacts SET state='ready' WHERE id=$1",artifact)
 exec("UPDATE photo.asset_versions SET review_state='pending' WHERE id=$1",historical);refuse(tid,pid,historical)
 exec("UPDATE photo.asset_versions SET review_state='accepted' WHERE id=$1",historical)
 exec("UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=$1",asset);refuse(tid,pid,historical)
}
