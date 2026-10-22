package collection

import (
 "testing"
 "strings"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/maintenance"
)
// Authored against current schema/API/maintenance roles; no execution in coding phase.
func TestIntegrationCollectionDeletionWithdrawsCatalogueAndPreservesCanonicalRoots(t *testing.T){
 ctx,admin,service,who,tid,id,other:=sectionAbandonFixture(t)
 var project,artifact,asset,version,item,otherItem,snapshot uuid.UUID;digest:=strings.Repeat("c",64)
 if err:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,id).Scan(&project);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,100,'image/jpeg','working_image','ready',now()) RETURNING id`,tid,uuid.NewString(),digest).Scan(&artifact);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tid,project,artifact).Scan(&asset);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,tid,asset,artifact,digest).Scan(&version);err!=nil{t.Fatal(err)}
 for _,pair:=range []struct{collection uuid.UUID;item *uuid.UUID}{{id,&item},{other,&otherItem}}{if err:=admin.QueryRow(ctx,`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES($1,$2,$3,$4,0) RETURNING id`,tid,pair.collection,asset,version).Scan(pair.item);err!=nil{t.Fatal(err)}}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES($1,'selection',$2,1,'user',$3,$4,1) RETURNING id`,tid,id,who.UserID,digest).Scan(&snapshot);err!=nil{t.Fatal(err)}
 if _,err:=admin.Exec(ctx,`INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES($1,$2,0,$3,$4,$5,$6,'{}')`,tid,snapshot,item,version,artifact,digest);err!=nil{t.Fatal(err)}
 accepted,err:=service.Archive(ctx,who,tid,id,1,DeleteRequest{ExpectedRevision:1,ConfirmResourceID:id});if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.deletion_jobs SET created_at='1970-01-01' WHERE id=$1`,accepted.JobID);err!=nil{t.Fatal(err)}
 // Freeze all existing asset/evidence rows and membership counts: collection
 // cleanup has no physical object or provenance deletion authority.
 var before string;if err=admin.QueryRow(ctx,`SELECT jsonb_build_object('assets',(SELECT count(*) FROM photo.assets WHERE tenant_id=$1),'versions',(SELECT count(*) FROM photo.asset_versions WHERE tenant_id=$1),'items',(SELECT count(*) FROM photo.collection_items WHERE tenant_id=$1),'snapshots',(SELECT count(*) FROM photo.snapshots WHERE tenant_id=$1),'artifacts',(SELECT count(*) FROM photo.artifacts WHERE tenant_id=$1))::text`,tid).Scan(&before);err!=nil{t.Fatal(err)}
 cfg,err:=pgxpool.ParseConfig(admin.Config().ConnString());if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_maintenance";pool,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer pool.Close()
 var outputRun uuid.UUID
 if err=admin.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,output_collection_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'apply','queued',$2,$3,'{}',$4,$5,true) RETURNING id`,tid,snapshot,id,digest,who.UserID).Scan(&outputRun);err!=nil{t.Fatal(err)}
 blocked,err:=(maintenance.CollectionCleaner{DB:pool}).CleanOne(ctx);if err!=nil||blocked{t.Fatalf("active output completed deletion: %v %v",blocked,err)}
 var cancelled,waiting bool;if err=admin.QueryRow(ctx,`SELECT r.cancel_requested AND r.status='cancelling',j.status='queued' AND j.collection_cleanup_retry_at>clock_timestamp() AND c.deleted_at IS NULL FROM photo.runs r JOIN photo.collections c ON c.tenant_id=r.tenant_id AND c.id=r.output_collection_id JOIN photo.deletion_jobs j ON j.tenant_id=c.tenant_id AND j.resource_id=c.id WHERE r.tenant_id=$1 AND r.id=$2 AND j.id=$3`,tid,outputRun,accepted.JobID).Scan(&cancelled,&waiting);err!=nil||!cancelled||!waiting{t.Fatalf("cooperative wait not recorded: %v",err)}
 // Simulate the existing scheduler's terminal acknowledgement, without any
 // direct task/quota/resource release by the collection cleaner.
 if _,err=admin.Exec(ctx,`UPDATE photo.runs SET status='cancelled' WHERE tenant_id=$1 AND id=$2`,tid,outputRun);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.deletion_jobs SET collection_cleanup_retry_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`,tid,accepted.JobID);err!=nil{t.Fatal(err)}
 finished,err:=(maintenance.CollectionCleaner{DB:pool}).CleanOne(ctx);if err!=nil||!finished{t.Fatalf("cleaner: %v %v",finished,err)}
 var deleted,completed bool;if err=admin.QueryRow(ctx,`SELECT c.deleted_at IS NOT NULL AND c.state='deleting',j.status='completed' AND j.cleanup_token IS NULL AND j.cleanup_lease_until IS NULL FROM photo.collections c JOIN photo.deletion_jobs j ON j.tenant_id=c.tenant_id AND j.resource_id=c.id WHERE c.tenant_id=$1 AND c.id=$2 AND j.id=$3`,tid,id,accepted.JobID).Scan(&deleted,&completed);err!=nil||!deleted||!completed{t.Fatalf("logical withdrawal not completed: %v",err)}
 if _,err=service.Get(ctx,who,tid,id);err==nil{t.Fatal("deleted collection still public owner catalogue")}
 if _,err=service.Get(ctx,who,tid,other);err!=nil{t.Fatalf("other collection lost authorized access: %v",err)}
 var exact bool;if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.collection_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id JOIN photo.artifacts f ON f.tenant_id=a.tenant_id AND f.id=a.original_artifact_id WHERE i.tenant_id=$1 AND i.id=$2 AND i.collection_id=$3 AND i.asset_id=$4 AND i.asset_version_id=$5 AND f.id=$6 AND f.state='ready') AND EXISTS(SELECT 1 FROM photo.snapshot_items si WHERE si.tenant_id=$1 AND si.snapshot_id=$7 AND si.item_id=$8 AND si.asset_version_id=$5 AND si.artifact_id=$6)`,tid,otherItem,other,asset,version,artifact,snapshot,item).Scan(&exact);err!=nil||!exact{t.Fatalf("shared asset or snapshot root lost: %v",err)}
 var after string;if err=admin.QueryRow(ctx,`SELECT jsonb_build_object('assets',(SELECT count(*) FROM photo.assets WHERE tenant_id=$1),'versions',(SELECT count(*) FROM photo.asset_versions WHERE tenant_id=$1),'items',(SELECT count(*) FROM photo.collection_items WHERE tenant_id=$1),'snapshots',(SELECT count(*) FROM photo.snapshots WHERE tenant_id=$1),'artifacts',(SELECT count(*) FROM photo.artifacts WHERE tenant_id=$1))::text`,tid).Scan(&after);err!=nil||after!=before{t.Fatalf("canonical roots changed: %s -> %s %v",before,after,err)}
}
func TestIntegrationCollectionDeletionRequiresLiveExactTenantJobResourceLease(t *testing.T){
 ctx,admin,service,who,tid,id,other:=sectionAbandonFixture(t);accepted,err:=service.Archive(ctx,who,tid,id,1,DeleteRequest{ExpectedRevision:1,ConfirmResourceID:id});if err!=nil{t.Fatal(err)}
 token:=uuid.New();if _,err=admin.Exec(ctx,`UPDATE photo.deletion_jobs SET status='running',cleanup_token=$2,cleanup_lease_until=clock_timestamp()+interval '1 minute' WHERE id=$1`,accepted.JobID,token);err!=nil{t.Fatal(err)}
 for _,args:=range [][]any{{uuid.New(),accepted.JobID,id,token},{tid,accepted.JobID,other,token},{tid,accepted.JobID,id,uuid.New()}}{var ok bool;if err=admin.QueryRow(ctx,`SELECT photo.finish_collection_deletion($1,$2,$3,$4)`,args...).Scan(&ok);err!=nil||ok{t.Fatalf("forged finalization: %v %v",ok,err)}}
 if _,err=admin.Exec(ctx,`UPDATE photo.deletion_jobs SET cleanup_lease_until=$2 WHERE id=$1`,accepted.JobID,time.Now().Add(-time.Minute));err!=nil{t.Fatal(err)}
 var ok bool;if err=admin.QueryRow(ctx,`SELECT photo.finish_collection_deletion($1,$2,$3,$4)`,tid,accepted.JobID,id,token).Scan(&ok);err!=nil||ok{t.Fatalf("expired finalization: %v %v",ok,err)}
 var visible bool;if err=admin.QueryRow(ctx,`SELECT deleted_at IS NULL FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,id).Scan(&visible);err!=nil||!visible{t.Fatal("invalid lease withdrew collection")}
}
func TestIntegrationCollectionDeletionPrivateMaintenanceACL(t *testing.T){
 ctx,admin,_,_,_,_,_:=sectionAbandonFixture(t)
 var claim,finish,apiClaim,apiFinish,publicClaim bool
 if err:=admin.QueryRow(ctx,`SELECT has_function_privilege('photo_maintenance','photo.claim_collection_deletion()','EXECUTE'),has_function_privilege('photo_maintenance','photo.finish_collection_deletion(uuid,uuid,uuid,uuid)','EXECUTE'),has_function_privilege('photo_api','photo.claim_collection_deletion()','EXECUTE'),has_function_privilege('photo_api','photo.finish_collection_deletion(uuid,uuid,uuid,uuid)','EXECUTE'),EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid='photo.claim_collection_deletion()'::regprocedure AND a.grantee=0 AND a.privilege_type='EXECUTE')`).Scan(&claim,&finish,&apiClaim,&apiFinish,&publicClaim);err!=nil{t.Fatal(err)}
 if !claim||!finish||apiClaim||apiFinish||publicClaim{t.Fatalf("cleanup ACL: %v %v %v %v %v",claim,finish,apiClaim,apiFinish,publicClaim)}
}

// Authored only: the lease is valid before the awaited collection lock and
// expires while blocked. Resuming the old function call must not publish.
func TestIntegrationCollectionDeletionRefusesLeaseExpiredDuringCollectionLock(t *testing.T){
 ctx,admin,service,who,tid,id,_:=sectionAbandonFixture(t)
 accepted,err:=service.Archive(ctx,who,tid,id,1,DeleteRequest{ExpectedRevision:1,ConfirmResourceID:id});if err!=nil{t.Fatal(err)}
 token:=uuid.New()
 blocker,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer blocker.Rollback(ctx)
 if _,err=blocker.Exec(ctx,`SELECT 1 FROM photo.collections WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,id);err!=nil{t.Fatal(err)}
 conn,err:=admin.Acquire(ctx);if err!=nil{t.Fatal(err)};defer func(){_ = blocker.Rollback(ctx);conn.Release()}();var pid int
 if err=conn.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.deletion_jobs SET status='running',cleanup_token=$2,cleanup_lease_until=clock_timestamp()+interval '500 milliseconds' WHERE id=$1`,accepted.JobID,token);err!=nil{t.Fatal(err)}
 type outcome struct{ok bool;err error};done:=make(chan outcome,1)
 go func(){var ok bool;e:=conn.QueryRow(ctx,`SELECT photo.finish_collection_deletion($1,$2,$3,$4)`,tid,accepted.JobID,id,token).Scan(&ok);done<-outcome{ok,e}}()
 // Observe the real second connection waiting, rather than assume goroutine
 // scheduling reached the row lock before the lease elapsed.
 deadline:=time.Now().Add(2*time.Second);waiting:=false
 for !waiting&&time.Now().Before(deadline){if err=admin.QueryRow(ctx,`SELECT cardinality(pg_blocking_pids($1))>0`,pid).Scan(&waiting);err!=nil{t.Fatal(err)};if !waiting{time.Sleep(time.Millisecond)}}
 if !waiting{t.Fatal("finalizer never reached awaited collection lock")}
 time.Sleep(600*time.Millisecond)
 if err=blocker.Commit(ctx);err!=nil{t.Fatal(err)}
 select{case result:=<-done:if result.err!=nil||result.ok{t.Fatalf("expired claimant finalized: %v %v",result.ok,result.err)};case <-time.After(2*time.Second):t.Fatal("finalizer did not resume")}
 var intact bool;if err=admin.QueryRow(ctx,`SELECT c.deleted_at IS NULL AND j.status='running' AND j.cleanup_token=$3 FROM photo.collections c JOIN photo.deletion_jobs j ON j.tenant_id=c.tenant_id AND j.resource_id=c.id WHERE c.tenant_id=$1 AND j.id=$2`,tid,accepted.JobID,token).Scan(&intact);err!=nil||!intact{t.Fatalf("expired claim changed publication/job: %v",err)}
}
