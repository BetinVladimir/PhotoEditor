from uuid import uuid4
from psycopg.types.json import Jsonb

def test_accepted_ingest_version_enqueues_exact_media_family(db):
 digest='a'*64
 user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone()
 tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
 project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Ingest media','workspace') RETURNING id",(tenant,)).fetchone()[0]
 artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'v1',%s,10,'application/json','working_image') RETURNING id",(tenant,f'masters/{uuid4()}',digest)).fetchone()[0]
 snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'input',0,'user',%s,%s,0) RETURNING id",(tenant,user,digest)).fetchone()[0]
 ingest=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'ingest','running',%s,'{}',%s,%s,true) RETURNING id",(tenant,snapshot,digest,user)).fetchone()[0]
 asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,artifact)).fetchone()[0]
 version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,created_by_run_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,%s,'{}','accepted') RETURNING id",(tenant,asset,artifact,ingest,digest)).fetchone()[0]
 job=db.execute("SELECT origin,bulk_operation_id,item_id,origin_run_id,asset_version_id,variants,status FROM photo.media_retry_jobs WHERE tenant_id=%s",(tenant,)).fetchone()
 assert job==('ingest',None,None,ingest,version,['thumbnail','preview'],'queued')
 claim=db.execute('SELECT event_id,materialize_token,tenant_id,bulk_id,item_id,asset_version_id,variants FROM photo.claim_media_retry_job()').fetchone()
 assert claim[2]==tenant and claim[3].int==0 and claim[4].int==0 and claim[5:]==(version,['thumbnail','preview'])
 event,token=claim[:2];delivery=uuid4();config={'event_id':str(event),'asset_version_id':str(version),'variants':['thumbnail','preview'],'srgb_sha256':'c'*64}
 assert db.execute('SELECT photo.begin_media_retry_plan(%s,%s,%s,%s,%s)',(event,token,delivery,'d'*64,Jsonb(config))).fetchone()[0]
 assert db.execute('SELECT kind,input_snapshot_id,created_by,graph_sealed FROM photo.runs WHERE id=%s',(delivery,)).fetchone()==('preview',snapshot,user,False)
 inputs=[{'artifact_id':str(artifact),'sha256':digest}]
 for index,variant in enumerate(('thumb320','thumb640','preview1600','preview2560')):
  node=uuid4();operation='media.thumbnail' if variant.startswith('thumb') else 'media.preview';params_sha=f'{index+1:x}'*64
  plan={'id':str(node),'asset_version_id':str(version),'variant':variant,'operation':operation,'profile_hash':'9'*64,'inputs':inputs,'params_sha256':params_sha}
  assert db.execute('SELECT photo.register_media_retry_node(%s,%s,%s,%s,%s)',(event,token,delivery,Jsonb(plan),event)).fetchone()[0]
  reservation=db.execute('SELECT outcome,artifact_id,token,ready FROM photo.reserve_media_retry_document(%s,%s,%s,%s,100,%s)',(tenant,delivery,node,params_sha,'photo')).fetchone()
  assert reservation[0]=='created'
  assert db.execute('SELECT photo.complete_media_retry_document(%s,%s,%s,%s,%s,%s,100,%s,%s)',(tenant,delivery,node,reservation[1],reservation[2],params_sha,'photo',f'params-{variant}')).fetchone()[0]
  task={'id':str(node),'node_key':f'media.retry/{event}/{variant}','operation':operation,'operation_version':'1','resource_class':'media','timeout_seconds':180 if operation=='media.thumbnail' else 300,'params':{'artifact_id':str(reservation[1]),'sha256':params_sha},'inputs':inputs,'input_digest':'f'*64,'execution_profile':{'backend':'cpu','precision':'float32','resource_estimate':{'ram_bytes':1,'vram_bytes':0,'scratch_bytes':1,'output_bytes':1,'pixel_count':1}}}
  assert db.execute('SELECT photo.materialize_media_retry_task(%s,%s,%s,%s)',(event,token,delivery,Jsonb(task))).fetchone()[0]
 assert db.execute('SELECT photo.seal_media_retry_plan(%s,%s,%s)',(event,token,delivery)).fetchone()[0]
 for task_id in [x[0] for x in db.execute('SELECT id FROM photo.tasks WHERE run_id=%s',(delivery,)).fetchall()]:
  derivative,manifest=uuid4(),uuid4();out_sha,manifest_sha='e'*64,'f'*64
  db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,%s,'photo',%s,'v1',%s,10,'image/jpeg','ready',%s,1,1,'derivative'),(%s,%s,'photo',%s,'v1',%s,10,'application/json','ready',%s,1,1,'derivative_manifest')",(derivative,tenant,f'd/{derivative}',out_sha,task_id,manifest,tenant,f'd/{manifest}',manifest_sha,task_id))
  outputs=[{'artifact_id':str(derivative),'sha256':out_sha},{'artifact_id':str(manifest),'sha256':manifest_sha}]
  result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task_id,Jsonb(outputs),'8'*64)).fetchone()[0]
  db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s WHERE id=%s",(result,task_id))
 assert db.execute('SELECT photo.finalize_media_retry_job()').fetchone()[0]
 assert db.execute('SELECT status FROM photo.media_retry_jobs WHERE event_id=%s',(event,)).fetchone()[0]=='completed'
 assert db.execute('SELECT count(*) FROM photo.derivatives WHERE tenant_id=%s AND asset_version_id=%s AND status=%s',(tenant,version,'ready')).fetchone()[0]==4

def test_pending_guest_ingest_does_not_enqueue_media(db):
 digest='b'*64
 user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone()
 tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
 project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Guest media','workspace') RETURNING id",(tenant,)).fetchone()[0]
 artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'v1',%s,10,'application/json','working_image') RETURNING id",(tenant,f'masters/{uuid4()}',digest)).fetchone()[0]
 snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'input',0,'user',%s,%s,0) RETURNING id",(tenant,user,digest)).fetchone()[0]
 ingest=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'ingest','running',%s,'{}',%s,%s,true) RETURNING id",(tenant,snapshot,digest,user)).fetchone()[0]
 asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,artifact)).fetchone()[0]
 db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,created_by_run_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,%s,'{}','pending_review')",(tenant,asset,artifact,ingest,digest))
 assert db.execute('SELECT count(*) FROM photo.media_retry_jobs WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0

def test_accepted_apply_version_enqueues_and_claims_media(db):
 digest='7'*64
 user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone()
 tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
 project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Apply media','workspace') RETURNING id",(tenant,)).fetchone()[0]
 artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'v1',%s,10,'application/json','working_image') RETURNING id",(tenant,f'masters/{uuid4()}',digest)).fetchone()[0]
 snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'input',0,'user',%s,%s,0) RETURNING id",(tenant,user,digest)).fetchone()[0]
 apply=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'apply','completed',%s,'{}',%s,%s,true) RETURNING id",(tenant,snapshot,digest,user)).fetchone()[0]
 asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,artifact)).fetchone()[0]
 version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,created_by_run_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,%s,'{}','pending_review') RETURNING id",(tenant,asset,artifact,apply,digest)).fetchone()[0]
 assert db.execute('SELECT count(*) FROM photo.media_retry_jobs WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0
 db.execute("UPDATE photo.asset_versions SET review_state='accepted' WHERE tenant_id=%s AND id=%s",(tenant,version))
 job=db.execute('SELECT origin,origin_run_id,asset_version_id,variants,status FROM photo.media_retry_jobs WHERE tenant_id=%s',(tenant,)).fetchone()
 assert job==('apply',apply,version,['thumbnail','preview'],'queued')
 claim=db.execute('SELECT event_id,materialize_token,bulk_id,item_id,asset_version_id FROM photo.claim_media_retry_job()').fetchone()
 assert claim[2].int==0 and claim[3].int==0 and claim[4]==version
 event,token=claim[:2];delivery=uuid4();config={'event_id':str(event),'asset_version_id':str(version),'variants':['thumbnail','preview'],'srgb_sha256':'c'*64}
 assert db.execute('SELECT photo.begin_media_retry_plan(%s,%s,%s,%s,%s)',(event,token,delivery,'d'*64,Jsonb(config))).fetchone()[0]
 assert db.execute('SELECT kind,input_snapshot_id,created_by FROM photo.runs WHERE id=%s',(delivery,)).fetchone()==('preview',snapshot,user)
