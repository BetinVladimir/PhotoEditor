package workflow

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// A usable decoded-image fixture first proves successful adoption, then rolls
// that savepoint back and archives the same destination before final adoption.
// This prevents an unrelated invalid fixture from masquerading as archive safety.
func TestIntegrationIngestArchiveAfterPreparedOriginal(t *testing.T) {
 for _, mode := range []string{"collection", "project", "project_no_collection"} { t.Run(mode,func(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn=="" {t.Skip("isolated PostgreSQL with migration2443 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil {t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil {t.Fatal(err)};defer tx.Rollback(ctx)
 exec:=func(q string,args ...any){t.Helper();if _,e:=tx.Exec(ctx,q,args...);e!=nil {t.Fatal(e)}}
 scan:=func(q string,dest any,args ...any){t.Helper();if e:=tx.QueryRow(ctx,q,args...).Scan(dest);e!=nil {t.Fatal(e)}}
 exec("SET CONSTRAINTS ALL DEFERRED")
 var user,tenant,project,collection,snapshot,run,source,original,master,pixels,reservation uuid.UUID
 scan("SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)",&user,uuid.NewString()+"@ingest-archive.test")
 scan("SELECT id FROM photo.tenants WHERE owner_user_id=$1",&tenant,user)
 scan("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Archive ingest','workspace') RETURNING id",&project,tenant)
 scan("INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Destination') RETURNING id",&collection,tenant,project)
 var destination *uuid.UUID=&collection
 if mode=="project_no_collection" {destination=nil}
 archive:=func(){if mode=="collection" {exec("UPDATE photo.collections SET state='archived' WHERE tenant_id=$1 AND id=$2",tenant,collection)} else {exec("UPDATE photo.projects SET archived_at=now() WHERE tenant_id=$1 AND id=$2",tenant,project)}}
 sha:=strings.Repeat("a",64);masterSHA:=strings.Repeat("b",64);pixelSHA:=strings.Repeat("c",64)
 artifact:=func(hash,media string,task *uuid.UUID,role *string)uuid.UUID{t.Helper();var id uuid.UUID;scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,created_by_task_id,output_role) VALUES($1,'test',$2,'v1',$3,3,$4,'ready',now(),$5,$6) RETURNING id`,&id,tenant,"archive-fixture/"+uuid.NewString(),hash,media,task,role);return id}
 sourceRole:="staging_upload"
 source=artifact(sha,"image/jpeg",nil,&sourceRole);original=artifact(sha,"image/jpeg",nil,nil)
 scan("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id",&snapshot,tenant,user,sha)
 exec("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload) VALUES($1,$2,0,$3,$4,'{}')",tenant,snapshot,source,sha)
 scan("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'ingest','running',$2,'{}',$3,$4,true) RETURNING id",&run,tenant,snapshot,sha,user)
 var sourceKey string
 scan("SELECT object_key FROM photo.artifacts WHERE tenant_id=$1 AND id=$2",&sourceKey,tenant,source)
 upload:=uuid.New()
 scan("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES($1,'user',$2,'upload',$3,3,1,'reserved',now()+interval '1 hour') RETURNING id",&reservation,tenant,user,upload)
 exec(`INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,expected_size,actual_size,filename,object_key,state,expires_at,reservation_id,staging_artifact_id,ingest_run_id,staging_candidate_id,ingest_snapshot_id) VALUES($1,$2,$3,$4,'user',$5,3,3,'frame.jpg',$6,'verifying',now()+interval '1 hour',$7,$8,$9,$8,$10)`,upload,tenant,project,destination,user,sourceKey,reservation,source,run,snapshot)
 exec("UPDATE photo.tenant_usage SET reserved_bytes=3,storage_bytes=3,reserved_files=1,source_files=0 WHERE tenant_id=$1",tenant)
 exec("INSERT INTO photo.ingest_upload_bindings(tenant_id,run_id,ordinal,upload_id,source_id,source_sha,project_id,collection_id,principal_type,principal_id) VALUES($1,$2,0,$3,$4,$5,$6,$7,'user',$8)",tenant,run,upload,source,sha,project,destination,user)
 exec("INSERT INTO photo.ingest_progress(tenant_id,run_id,phase,item_count) VALUES($1,$2,'catalog',1)",tenant,run)
 exec("INSERT INTO photo.ingest_originals(tenant_id,run_id,ordinal,source_id,source_sha,artifact_id,token,bytes,bucket,media_type,state,object_version,completed_at) VALUES($1,$2,0,$3,$4,$5,$6,3,'test','image/jpeg','ready','v1',now())",tenant,run,source,sha,original,uuid.New())
 task,result:=uuid.New(),uuid.New();roleImage,rolePixels:="working_image","working_rgb"
 master=artifact(masterSHA,"application/json",&task,&roleImage);pixels=artifact(pixelSHA,"application/octet-stream",&task,&rolePixels)
 exec(`INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,canonical_result_id) VALUES($1,$2,$3,'asset.decode/0000','asset.decode','1.0.0','cpu','succeeded','[]',$4,$5,'{}',$6)`,task,tenant,run,source,sha,result)
 exec(`INSERT INTO photo.ingest_node_plans(id,tenant_id,run_id,ordinal,node_key,operation,source_artifact_id,source_sha,params_sha,params) VALUES($1,$2,$3,0,'asset.decode/0000','asset.decode',$4,$5,$5,'{}')`,task,tenant,run,source,sha)
 exec(`INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,$3,0,0,jsonb_build_array(jsonb_build_object('artifact_id',$4::text,'sha256',$5::text),jsonb_build_object('artifact_id',$6::text,'sha256',$7::text)),'{}',$5)`,result,tenant,task,master,masterSHA,pixels,pixelSHA)
 image:=`{"canvas":{"width":1,"height":1,"source_sha256":"`+sha+`"},"pixels":{"artifact_id":"`+pixels.String()+`","sha256":"`+pixelSHA+`"},"alpha":null,"dtype":"float32","color":{"profile_id":"work_rec2020_gamma24_v1"}}`
 metadata:=`{"source":{"artifact_id":"`+source.String()+`","sha256":"`+sha+`"},"width":1,"height":1,"orientation":1,"has_alpha":false,"format":"jpeg","sensor_alignment":null}`
 assertFiles:=func(wantReserved,wantSource int64){t.Helper();var reservedFiles,sourceFiles int64
  if err=tx.QueryRow(ctx,"SELECT reserved_files,source_files FROM photo.tenant_usage WHERE tenant_id=$1",tenant).Scan(&reservedFiles,&sourceFiles);err!=nil||reservedFiles!=wantReserved||sourceFiles!=wantSource{t.Fatal("upload file counters changed incorrectly",err,reservedFiles,sourceFiles,wantReserved,wantSource)}
 }
 assertFiles(1,0)
 var ok bool
 adopt:=func(){scan("SELECT photo.adopt_ingest_image($1,$2,0,$3,$4,$5::jsonb,$6::jsonb)",&ok,tenant,run,master,masterSHA,image,metadata)}
 exec("SAVEPOINT live_destination");adopt();if !ok {t.Fatal("positive live adoption fixture failed")};assertFiles(0,1)
 archive()
 adopt();if !ok {t.Fatal("settled receipt replay failed after archive")};assertFiles(0,1)
 exec("ROLLBACK TO SAVEPOINT live_destination");assertFiles(1,0)
 scan("SELECT photo.request_archived_ingest_cancel($1,$2)",&ok,tenant,run);if ok {t.Fatal("live destination incorrectly cancelled")}
 archive()
 adopt();if ok {t.Fatal("archived destination adopted")};assertFiles(1,0)
 var count int;scan("SELECT count(*) FROM photo.ingest_catalog_items WHERE tenant_id=$1 AND run_id=$2",&count,tenant,run);if count!=0 {t.Fatal("partial catalog")}
 scan("SELECT count(*) FROM photo.collection_items WHERE tenant_id=$1 AND collection_id=$2",&count,tenant,collection);if count!=0 {t.Fatal("partial membership")}
 var reserved,storage int64
 if err=tx.QueryRow(ctx,"SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=$1",tenant).Scan(&reserved,&storage);err!=nil||reserved!=3||storage!=3 {t.Fatal("refused adoption changed quota",err,reserved,storage)}
 exec("SET LOCAL ROLE photo_scheduler")
 scan("SELECT photo.request_archived_ingest_cancel($1,$2)",&ok,tenant,run);if !ok {t.Fatal("archive did not request cancellation")}
 scan("SELECT photo.cancel_ingest_upload()",&count);if count!=1 {t.Fatal("prepared original prevented cancellation settlement")};assertFiles(0,0)
 scan("SELECT photo.cancel_ingest_upload()",&count);if count!=0 {t.Fatal("cancellation settlement repeated")};assertFiles(0,0)
 scan("SELECT photo.finalize_run_cancellation()",&count);if count!=1 {t.Fatal("settled ingest did not finalize cancellation")}
 exec("RESET ROLE")
 var runState string;scan("SELECT status FROM photo.runs WHERE tenant_id=$1 AND id=$2",&runState,tenant,run);if runState!="cancelled" {t.Fatal(runState)}
 if err=tx.QueryRow(ctx,"SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=$1",tenant).Scan(&reserved,&storage);err!=nil||reserved!=0||storage!=6 {t.Fatal("cancelled staging accounting",err,reserved,storage)}
 assertFiles(0,0)
 var allowed bool
 scan("SELECT has_function_privilege('photo_api','photo.request_archived_ingest_cancel(uuid,uuid)','EXECUTE')",&allowed);if allowed {t.Fatal("API can invoke scheduler cancellation")}
 var state string;scan("SELECT state FROM photo.upload_sessions WHERE tenant_id=$1 AND id=$2",&state,tenant,upload);if state!="rejected" {t.Fatal(state)}
 scan("SELECT count(*) FROM photo.staging_cleanup WHERE tenant_id=$1 AND run_id=$2 AND artifact_id=$3 AND state='queued'",&count,tenant,run,source);if count!=1 {t.Fatal("staging cleanup not queued")}
 scan("SELECT count(*) FROM photo.ingest_originals WHERE tenant_id=$1 AND run_id=$2 AND state='ready'",&count,tenant,run);if count!=1 {t.Fatal("prepared original unexpectedly deleted")}
 }) }
}
