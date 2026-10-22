package run

import (
 "context"
 "encoding/json"
 "os"
 "strconv"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
)
func TestIntegrationApplyAdvisoryRechecksProjectAfterStyleWait(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")};ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close();cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api";api,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer api.Close()
 var owner,actor,tid,session,projectID,collection,style,version,artifact uuid.UUID;var epoch,revision int64
 query:=func(sql string,args []any,dest ...any){t.Helper();if e:=admin.QueryRow(ctx,sql,args...).Scan(dest...);e!=nil{t.Fatal(e)}};exec:=func(sql string,args ...any){t.Helper();if _,e:=admin.Exec(ctx,sql,args...);e!=nil{t.Fatal(e)}}
 query(`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,[]any{uuid.NewString()+"@test.example"},&owner);query(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,[]any{owner},&tid)
 query(`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,[]any{uuid.NewString()+"@test.example"},&actor,&epoch);exec(`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')`,tid,actor)
 query(`INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'native',now()+interval '1 hour') RETURNING id`,[]any{actor,auth.HashSecret(uuid.NewString()),uuid.New()},&session)
 query(`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Apply authority','restricted') RETURNING id`,[]any{tid},&projectID)
 query(`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Source') RETURNING id,revision`,[]any{tid,projectID},&collection,&revision)
 hash,e:=canonical.Hash(json.RawMessage(identityStyleJSON));if e!=nil{t.Fatal(e)}
 query(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,$4,'application/json','style','ready',now()) RETURNING id`,[]any{tid,uuid.NewString(),hash,len(identityStyleJSON)},&artifact)
 query(`INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,'Identity',$3) RETURNING id`,[]any{tid,projectID,owner},&style)
 query(`INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES($1,$2,1,$3::jsonb,$4,'2.0.0','photo_renderer_v2','accepted','{}',$5) RETURNING id`,[]any{tid,style,string(identityStyleJSON),hash,artifact},&version)
 who:=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch};svc:=Service{DB:api};rev:=strconv.FormatInt(revision,10)
 first,e:=svc.CollectionApplyCapabilities(ctx,who,tid,collection,version,rev);if e!=nil||!first.Available{t.Fatal("identity advisory should need no fabricated worker",first,e)}
 exec(`UPDATE photo.collections SET state='deleting' WHERE tenant_id=$1 AND id=$2`,tid,collection)
 if _,e=svc.CollectionApplyCapabilities(ctx,who,tid,collection,version,rev);domain.AsError(e).Code!="REVISION_CONFLICT"{t.Fatal("deleting must refuse fresh advisory",e)}
 exec(`UPDATE photo.collections SET state='draft' WHERE tenant_id=$1 AND id=$2`,tid,collection)
 query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision);rev=strconv.FormatInt(revision,10)
 holder,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer holder.Rollback(context.WithoutCancel(ctx));var blocker int;if e=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);e!=nil{t.Fatal(e)};if _,e=holder.Exec(ctx,`LOCK TABLE photo.style_versions IN ACCESS EXCLUSIVE MODE`);e!=nil{t.Fatal(e)}
 done:=make(chan error,1);go func(){_,err:=svc.CollectionApplyCapabilities(ctx,who,tid,collection,version,rev);done<-err}()
 tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();deadline:=time.NewTimer(3*time.Second);defer deadline.Stop();observed:=false
 for !observed{select{case err:=<-done:t.Fatal("no actual style lock wait",err);case <-deadline.C:t.Fatal("style wait not observed");case <-tick.C:query(`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,[]any{blocker},&observed)}}
 exec(`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tid,actor);if e=holder.Commit(ctx);e!=nil{t.Fatal(e)}
 select{case e=<-done:if e==nil{t.Fatal("old Admin role leaked restricted project advisory")};case <-ctx.Done():t.Fatal(ctx.Err())}
}
