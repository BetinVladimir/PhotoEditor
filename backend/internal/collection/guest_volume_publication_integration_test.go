package collection

import (
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Authored only: a ready guest direct-file volume has a genuine current encode
// producer/result. Positive assertions precede unpublication so a missing
// producer or malformed fixture cannot make the revocation check vacuous.
func TestIntegrationUnpublishFencesGuestVolumeIntentsAndPostSigning(t *testing.T){
 ctx,admin,service,who,tid,collection,_:=sectionAbandonFixture(t)
 service.Pepper=[]byte(strings.Repeat("p",32));req:=shareIntentRequest();req.Permissions=SharePermissions{View:true,Download:true,Upload:true}
 share,err:=service.CreateShare(ctx,who,tid,collection,uuid.NewString(),req);if err!=nil{t.Fatal(err)}
 sessionHash:=[]byte(strings.ReplaceAll(uuid.NewString(),"-",""));csrfHash:=[]byte(strings.ReplaceAll(uuid.NewString(),"-",""));digest:=strings.Repeat("a",64)
 var session,snapshot,run,params,task,artifact,userArtifact,result,job uuid.UUID
 if err=admin.QueryRow(ctx,`INSERT INTO photo.share_sessions(tenant_id,share_id,collection_id,session_hash,link_version,expires_at,locale,csrf_hash) VALUES($1,$2,$3,$4,1,clock_timestamp()+interval '1 hour','en',$5) RETURNING id`,tid,share.ID,collection,sessionHash,csrfHash).Scan(&session);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES($1,'selection',$2,2,'guest',$3,$4,0) RETURNING id`,tid,collection,session,digest).Scan(&snapshot);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'download','completed',$2,'{}',$3,$4,true) RETURNING id`,tid,snapshot,digest,who.UserID).Scan(&run);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,2,'application/json','task_params','ready',now()) RETURNING id`,tid,uuid.NewString(),digest).Scan(&params);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'encode','export.encode','1','cpu','ready','[]',$3,$4,1,1,'{}') RETURNING id`,tid,run,params,digest).Scan(&task);err!=nil{t.Fatal(err)}
 objectKey:=uuid.NewString()
 if err=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at,expires_at,created_by_task_id,produced_attempt,produced_fence) VALUES($1,'photo',$2,'v1',$3,16,'image/jpeg','encoded_image','ready',now(),clock_timestamp()+interval '1 hour',$4,1,1) RETURNING id`,tid,objectKey,digest,task).Scan(&artifact);err!=nil{t.Fatal(err)}
 userObjectKey:=uuid.NewString()
 if err=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at,expires_at,created_by_task_id,produced_attempt,produced_fence) VALUES($1,'photo',$2,'v1',$3,16,'image/jpeg','encoded_image','ready',now(),clock_timestamp()+interval '1 hour',$4,1,1) RETURNING id`,tid,userObjectKey,digest,task).Scan(&userArtifact);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text),jsonb_build_object('artifact_id',$5::text,'sha256',$4::text)),'{}',$4) RETURNING id`,tid,task,artifact,digest,userArtifact).Scan(&result);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.tasks SET canonical_result_id=$3,state='succeeded' WHERE tenant_id=$1 AND id=$2`,tid,task,result);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.download_jobs(tenant_id,collection_id,share_id,principal_type,principal_id,policy_version,snapshot_id,preset,status,expires_at,run_id) VALUES($1,$2,$3,'guest',$4,1,$5,'{}','ready',clock_timestamp()+interval '1 hour',$6) RETURNING id`,tid,collection,share.ID,session,snapshot,run).Scan(&job);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.download_volumes(tenant_id,download_job_id,volume_no,artifact_id,status,files_done,bytes_written) VALUES($1,$2,1,$3,'ready',1,16)`,tid,job,artifact);err!=nil{t.Fatal(err)}
 cfg,err:=pgxpool.ParseConfig(admin.Config().ConnString());if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_portal";portal,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer portal.Close()
 create:=func(key string,token []byte)string{t.Helper();var outcome string;if e:=portal.QueryRow(ctx,`SELECT outcome FROM photo.create_portal_download_intent($1,$2,$3,$4,$5,1,$6,$7,$8)`,sessionHash,csrfHash,key,digest,job,uuid.New(),token,time.Now().Add(50*time.Second)).Scan(&outcome);e!=nil{t.Fatal(e)};return outcome}
 firstHash:=[]byte(strings.ReplaceAll(uuid.NewString(),"-",""));pendingHash:=[]byte(strings.ReplaceAll(uuid.NewString(),"-",""));firstKey:=uuid.NewString()
 if outcome:=create(firstKey,firstHash);outcome!="created"{t.Fatalf("published admission: %s",outcome)}
 if outcome:=create(firstKey,[]byte(strings.Repeat("r",32)));outcome!="replayed"{t.Fatalf("published replay: %s",outcome)}
 if outcome:=create(uuid.NewString(),pendingHash);outcome!="created"{t.Fatalf("pending bearer: %s",outcome)}
 var outcome string;if err=portal.QueryRow(ctx,`SELECT outcome FROM photo.redeem_download_intent($1,'photo')`,firstHash).Scan(&outcome);err!=nil||outcome!="ok"{t.Fatalf("published redemption: %s %v",outcome,err)}
 current:=func()bool{t.Helper();var value bool;if e:=portal.QueryRow(ctx,`SELECT photo.v30_delivery_download_intent_current($1,'photo',$2,'v1',$3,16,'image/jpeg')`,firstHash,objectKey,digest).Scan(&value);e!=nil{t.Fatal(e)};return value}
 if !current(){t.Fatal("published producer-backed post-sign fixture invalid")}
 if _,err=service.Publish(ctx,who,tid,collection,PublicationRequest{ExpectedRevision:2,Publish:false,PublishCollection:true});err!=nil{t.Fatal(err)}
 if outcome=create(firstKey,[]byte(strings.Repeat("x",32)));outcome!="unauthorized"{t.Fatalf("unpublished replay issued bearer: %s",outcome)}
 if outcome=create(uuid.NewString(),[]byte(strings.Repeat("n",32)));outcome!="unauthorized"{t.Fatalf("unpublished fresh admission: %s",outcome)}
 if err=portal.QueryRow(ctx,`SELECT outcome FROM photo.redeem_download_intent($1,'photo')`,pendingHash).Scan(&outcome);err!=nil||outcome!="revoked"{t.Fatalf("unpublished bearer redeemed: %s %v",outcome,err)}
 if current(){t.Fatal("unpublished collection passed post-sign publication fence")}
 // Authenticated owner volumes retain their independent project authority on
 // draft collections; the new gate must not run in that branch.
 var userJob uuid.UUID;userHash:=[]byte(strings.ReplaceAll(uuid.NewString(),"-",""))
 if err=admin.QueryRow(ctx,`INSERT INTO photo.download_jobs(tenant_id,collection_id,principal_type,principal_id,snapshot_id,preset,status,expires_at,run_id) VALUES($1,$2,'user',$3,$4,'{}','ready',clock_timestamp()+interval '1 hour',$5) RETURNING id`,tid,collection,who.UserID,snapshot,run).Scan(&userJob);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.download_volumes(tenant_id,download_job_id,volume_no,artifact_id,status,files_done,bytes_written) VALUES($1,$2,1,$3,'ready',1,16)`,tid,userJob,userArtifact);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.download_intents(tenant_id,principal_type,principal_id,artifact_id,token_hash,expires_at) VALUES($1,'user',$2,$3,$4,clock_timestamp()+interval '50 seconds')`,tid,who.UserID,userArtifact,userHash);err!=nil{t.Fatal(err)}
 if err=portal.QueryRow(ctx,`SELECT outcome FROM photo.redeem_download_intent($1,'photo')`,userHash).Scan(&outcome);err!=nil||outcome!="ok"{t.Fatalf("draft owner volume refused: %s %v",outcome,err)}
 var userCurrent bool;if err=portal.QueryRow(ctx,`SELECT photo.v30_delivery_download_intent_current($1,'photo',$2,'v1',$3,16,'image/jpeg')`,userHash,userObjectKey,digest).Scan(&userCurrent);err!=nil||!userCurrent{t.Fatalf("draft owner post-sign refused: %v %v",userCurrent,err)}
 // Draft upload capability remains independent; no generic session revoke.
 var uploads int;if err=portal.QueryRow(ctx,`SELECT count(*) FROM photo.authorize_portal_session($1,$2,'upload')`,sessionHash,csrfHash).Scan(&uploads);err!=nil||uploads!=1{t.Fatalf("draft upload authorization lost: %d %v",uploads,err)}
 // The private gate is not an additional callable cross-tenant projection.
 var allowed bool;if err=admin.QueryRow(ctx,`SELECT has_function_privilege('photo_portal','photo.guest_volume_collection_current(uuid,uuid)','EXECUTE') OR has_function_privilege('photo_api','photo.guest_volume_collection_current(uuid,uuid)','EXECUTE')`).Scan(&allowed);err!=nil||allowed{t.Fatal("private gate exposed",err)}
}
