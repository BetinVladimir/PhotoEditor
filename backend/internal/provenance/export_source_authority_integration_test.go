package provenance

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

// Uses the full canonical export join and the actual API role. Physical object
// storage and native signing are outside this relational authorization scenario.
func TestIntegrationSigningCatalogRefreshesAdminAfterCanonicalShareWait(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 app:="signing-catalog-wait-"+uuid.NewString()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)}
 cfg.ConnConfig.RuntimeParams["role"]="photo_api";cfg.ConnConfig.RuntimeParams["application_name"]=app
 api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
 exec:=func(t *testing.T,query string,args ...any){t.Helper();if _,err:=db.Exec(ctx,query,args...);err!=nil{t.Fatal(err)}}
 scan:=func(t *testing.T,query string,dest any,args ...any){t.Helper();if err:=db.QueryRow(ctx,query,args...).Scan(dest);err!=nil{t.Fatal(err)}}
	var user, tid, session, project, snapshot, run, params, task, source, result, asset, version, export uuid.UUID
	var epoch int64
	if err := db.QueryRow(ctx, `SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@lineage.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	scan(t, `SELECT id FROM photo.tenants WHERE owner_user_id=$1`, &tid, user)
 var actor uuid.UUID;var actorEpoch int64
 if err:=db.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@signing-admin.test").Scan(&actor,&actorEpoch);err!=nil{t.Fatal(err)}
 exec(t,`INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'admin')`,tid,actor)
 user=actor;epoch=actorEpoch
	session = uuid.New()
	exec(t, `INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`, session, user, auth.HashSecret(uuid.NewString()), uuid.New())
	exec(t, `UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1000000'::jsonb) WHERE id=$1`, tid)
	scan(t, `INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES($1,'Lineage SQL fixture','restricted',true) RETURNING id`, &project, tid)
	sha := strings.Repeat("b", 64)
	scan(t, `INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id`, &snapshot, tid, user, sha)
	scan(t, `INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'preview','completed',$2,'{}',$3,$4) RETURNING id`, &run, tid, snapshot, sha, user)
	scan(t, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,state) VALUES($1,'test-bucket',$2,$3,2,'application/json','params-v1',now(),'ready') RETURNING id`, &params, tid, uuid.NewString(), sha)
	scan(t, `INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'media.preview:0','media.preview','1','media','ready','[]',$3,$4,1,1,'{"renderer_version":"v2.2"}') RETURNING id`, &task, tid, run, params, sha)
	exec(t, `INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,1,$3,now(),now(),'cpu','{}')`, tid, task, uuid.New())
	sourceKey := "lineage-source/" + uuid.NewString()
	scan(t, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence,state) VALUES($1,'test-bucket',$2,'source-v1',$3,42,'image/jpeg','derivative',now(),$4,1,1,'ready') RETURNING id`, &source, tid, sourceKey, sha, task)
	scan(t, `INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text)),'{}',$4) RETURNING id`, &result, tid, task, source, sha)
	exec(t, `UPDATE photo.tasks SET state='succeeded',canonical_result_id=$1 WHERE id=$2`, result, task)
	scan(t, `INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, &asset, tid, project, source)
	scan(t, `INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,created_by_run_id) VALUES($1,$2,$3,$4,'{}','accepted',$5) RETURNING id`, &version, tid, asset, source, sha, run)
	scan(t, `INSERT INTO photo.exports(tenant_id,asset_version_id,preset_hash,artifact_id,status,task_id,metadata_policy) VALUES($1,$2,$3,$4,'ready',$5,'{}') RETURNING id`, &export, tid, version, sha, source, task)
	

 who:=auth.Identity{UserID:user,SessionID:session,Epoch:epoch}
 catalog:=SQLSigningExportCatalog{DB:api,Bucket:"test-bucket"}
 positive,err:=catalog.LookupSigningExport(ctx,who,tid,export)
 if err!=nil||positive.ExportID!=export||positive.Ref.ArtifactID!=source{t.Fatal("canonical positive fixture refused",err)}
 blocker,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer blocker.Rollback(context.WithoutCancel(ctx))
 if _,err=blocker.Exec(ctx,`SELECT id FROM photo.exports WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,export);err!=nil{t.Fatal(err)}
 done:=make(chan error,1)
 go func(){_,err:=catalog.LookupSigningExport(ctx,who,tid,export);done<-err}()
 deadline:=time.Now().Add(3*time.Second)
 for {
  var waiting bool
  if err:=db.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE '%FOR SHARE OF e,v,asset%')`,app).Scan(&waiting);err!=nil{t.Fatal(err)}
  if waiting{break}
  select{case err:=<-done:t.Fatal("canonical share wait was not reached",err);default:}
  if time.Now().After(deadline){t.Fatal("canonical share wait not observed")};time.Sleep(10*time.Millisecond)
 }
 exec(t,`UPDATE photo.memberships SET role='editor',revision=revision+1 WHERE tenant_id=$1 AND user_id=$2`,tid,user)
 if err:=blocker.Commit(ctx);err!=nil{t.Fatal(err)}
 if err:=<-done;err==nil||domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("stale Admin bypassed current restricted project",err)}
}
