package interchange

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Reproducible persisted source inventory for the installed grant function.
// This seeds already-adopted metadata; it does not pretend to execute the image
// decoder, parse worker, storage gateway or independent adoption verifier.
func seedImportGrantSources(t *testing.T,ctx context.Context,db *pgxpool.Pool,tid,project,actor,session,workflow,run,snapshot,asset,master,entry uuid.UUID,masterSHA string)(uuid.UUID,uuid.UUID,string,string,string){
 t.Helper()
 exec:=func(q string,args ...any){t.Helper();if _,e:=db.Exec(ctx,q,args...);e!=nil{t.Fatal(e)}}
 scan:=func(q string,dst any,args ...any){t.Helper();if e:=db.QueryRow(ctx,q,args...).Scan(dst);e!=nil{t.Fatal(e)}}
 encode:=func(v any)string{b,e:=json.Marshal(v);if e!=nil{t.Fatal(e)};return string(b)}
 digest:=func(s string)string{h:=sha256.Sum256([]byte(s));return hex.EncodeToString(h[:])}
 ref:=func(id uuid.UUID,sha string)map[string]any{return map[string]any{"artifact_id":id,"sha256":sha}}
 var child uuid.UUID
 scan(`SELECT photo.v30_uuid5($1,'interchange:canonical-ingest:1')`,&child,workflow)
 exec(`INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,$2,'ingest','completed',$3,'{}',$4,$5,true)`,child,tid,snapshot,masterSHA,actor)
 var version uuid.UUID
 scan(`INSERT INTO photo.asset_versions(tenant_id,asset_id,version_no,master_artifact_id,content_hash,metadata,review_state,created_by_run_id) VALUES($1,$2,2,$3,$4,'{}','accepted',$5) RETURNING id`,&version,tid,asset,master,masterSHA,child)
 exec(`UPDATE photo.artifacts SET verified_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`,tid,master)
 parsed:=encode(map[string]any{"manifest":map[string]any{"entries":[]any{map[string]any{"entry_id":entry,"image_path":"photo.jpg","image_sha256":masterSHA}}},"files":[]any{map[string]any{"path":"photo.jpg","content":ref(master,masterSHA)}}})
 parsedSHA:=digest(parsed);params,task,root,result:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 exec(`UPDATE photo.runs SET status='running',graph_sealed=true WHERE tenant_id=$1 AND id=$2`,tid,run)
 exec(`INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES($1,$2,'photo',$3,'params-v1',$4,2,'application/json','params',clock_timestamp())`,params,tid,"params/"+params.String(),masterSHA)
 exec(`INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,$3,'v2.interchange.parse:0','v2.interchange.parse','1','cpu','blocked','[]',$4,$5,1,1,'{"renderer_version":"v2.2"}')`,task,tid,run,params,masterSHA)
 exec(`INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES($1,$2,'photo',$3,'parsed-v1',$4,$5,'application/json','document',clock_timestamp(),$6,1,1)`,root,tid,"parsed/"+root.String(),parsedSHA,len(parsed),task)
 // The committed parse inventory includes the root and one archived source.
 for _,out:=range []struct{id uuid.UUID;sha,role string;size int64}{{root,parsedSHA,"document",int64(len(parsed))},{master,masterSHA,"archive_file",8}}{
  exec(`INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT $1,$2,1,1,$3,$4,$5,a.id,$6,a.bucket,a.object_key,a.sha256,a.bytes,a.media_type,'ready',a.object_version,clock_timestamp() FROM photo.artifacts a WHERE a.tenant_id=$1 AND a.id=$7`,tid,task,uuid.New(),masterSHA,uuid.New(),out.role,out.id)
 }
 exec(`INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,$3,1,1,$4::jsonb,'{}',$5)`,result,tid,task,encode([]any{ref(root,parsedSHA)}),masterSHA)
 exec(`UPDATE photo.tasks SET canonical_result_id=$1,state='succeeded' WHERE tenant_id=$2 AND id=$3`,result,tid,task)
 for i,out:=range []struct{id uuid.UUID;sha string}{{root,parsedSHA},{master,masterSHA}}{
  path:="tasks/"+task.String()+"/$";if i==1{path+="/files/0/content"}
  exec(`INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) VALUES($1,$2,$3,$4,$5,$6,$7)`,tid,project,workflow,out.id,out.sha,digest(path),i)
 }
 var event uuid.UUID;scan(`SELECT photo.v2_adoption_event_id($1,$2)`,&event,task,result)
 for _,consumer:=range []string{"v2-adopt","v30-interchange-ingest-started","v30-interchange-parsed"}{exec(`INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES($1,$2,$3,clock_timestamp())`,tid,consumer,event)}
 exec(`INSERT INTO photo.run_events(tenant_id,cursor,run_id,audience,type,payload) VALUES($1,1,$2,'owner','v2.task.adopted',$3::jsonb)`,tid,run,encode(map[string]any{"task_id":task,"result_id":result,"attempt":1,"fence":1,"inventory_digest":masterSHA,"inventory_count":2}))
 proof:=encode(map[string]any{"auth_session_id":session,"project_id":project})
 exec(`INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,v30_interchange_proof) VALUES($1,$2,$3,'task_params',$4,$5,$6,2,'photo','ready','params-v1',clock_timestamp(),$7::jsonb)`,tid,run,task,params,uuid.New(),masterSHA,proof)
 exec(`INSERT INTO photo.v30_interchange_ingest_bindings(tenant_id,run_id,ordinal,workflow_id,project_id,actor_id,parsed_id,parsed_sha,entry_id,external_id,source_id,source_sha,asset_id,version_id,item_id) VALUES($1,$2,0,$3,$4,$5,$6,$7,$8,'photo',$9,$10,$11,$12,$13)`,tid,child,workflow,project,actor,root,parsedSHA,entry,master,masterSHA,asset,version,uuid.New())
 exec(`INSERT INTO photo.v30_interchange_ingest_outcomes(tenant_id,run_id,ordinal,state,master_id,master_sha) VALUES($1,$2,0,'adopted',$3,$4)`,tid,child,master,masterSHA)
 exec(`INSERT INTO photo.ingest_progress(tenant_id,run_id,phase,item_count) VALUES($1,$2,'complete',1)`,tid,child)
 proposal:=uuid.New();proposalRaw:=encode(map[string]any{"algorithm":"sensor_dust_log_v1","frames":[]any{ref(master,masterSHA)},"candidates":[]any{}});proposalSHA:=digest(proposalRaw)
 exec(`INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES($1,$2,'photo',$3,'proposal-v1',$4,$5,'application/json','v30_import_prepared',clock_timestamp())`,proposal,tid,"prepared/"+proposal.String(),proposalSHA,len(proposalRaw))
 scope:=encode(map[string]any{"contract":"DustCandidates","workflow":workflow,"entry":entry,"binding":map[string]any{"asset_version_id":version,"source":ref(master,masterSHA)}})
 exec(`INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,v30_import_prepared) VALUES($1,$2,$3,'v30_import_prepared',$3,$4,$5,$6,'photo','ready','proposal-v1',clock_timestamp(),$7::jsonb)`,tid,run,proposal,uuid.New(),proposalSHA,len(proposalRaw),scope)
 return version,proposal,proposalSHA,parsed,proposalRaw
}
