from uuid import uuid4
from psycopg.types.json import Jsonb


def test_media_retry_command_intake_is_exact_and_replayable(db):
    digest='a'*64
    user,epoch=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone()
    tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
    project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Retry','workspace') RETURNING id",(tenant,)).fetchone()[0]
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'v1',%s,10,'application/json','working_image') RETURNING id",(tenant,f'masters/{uuid4()}',digest)).fetchone()[0]
    asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,artifact)).fetchone()[0]
    version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tenant,asset,artifact,digest)).fetchone()[0]
    collection=db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) VALUES(%s,%s,'Retry') RETURNING id",(tenant,project)).fetchone()[0]
    item=db.execute("INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,moderation_state) VALUES(%s,%s,%s,%s,0,'accepted') RETURNING id",(tenant,collection,asset,version)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'selection',%s,1,'user',%s,%s,1) RETURNING id",(tenant,collection,user,digest)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,%s,'{}')",(tenant,snapshot,item,version,artifact,digest))
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'bulk','completed',%s,'{}',%s,%s,true) RETURNING id",(tenant,snapshot,digest,user)).fetchone()[0]
    variants=['thumbnail','preview']
    bulk=db.execute("INSERT INTO photo.bulk_operations(tenant_id,collection_id,snapshot_id,action,params,expected_next_revision,status,created_by,run_id) VALUES(%s,%s,%s,'retry_media',%s,1,'completed',%s,%s) RETURNING id",(tenant,collection,snapshot,Jsonb({'variants':variants}),user,run)).fetchone()[0]
    db.execute("INSERT INTO photo.bulk_item_results(tenant_id,bulk_operation_id,snapshot_ordinal,status,result) VALUES(%s,%s,0,'succeeded','{}')",(tenant,bulk))
    event=uuid4();payload={'schema_version':1,'event_id':str(event),'bulk_id':str(bulk),'item_id':str(item),'asset_version_id':str(version),'variants':variants}
    db.execute("INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(%s,%s,'photo.commands.v1','media.retry',%s)",(tenant,event,Jsonb(payload)))
    assert db.execute('SELECT photo.accept_media_retry_command(%s,%s,%s,%s,%s)',(event,bulk,item,version,Jsonb(variants))).fetchone()[0]
    assert db.execute('SELECT photo.accept_media_retry_command(%s,%s,%s,%s,%s)',(event,bulk,item,version,Jsonb(variants))).fetchone()[0]
    stored=db.execute('SELECT tenant_id,bulk_operation_id,item_id,asset_version_id,variants,status FROM photo.media_retry_jobs WHERE event_id=%s',(event,)).fetchone()
    assert stored==(tenant,bulk,item,version,variants,'queued')
    assert not db.execute('SELECT photo.accept_media_retry_command(%s,%s,%s,%s,%s)',(uuid4(),bulk,item,version,Jsonb(variants))).fetchone()[0]
    claimed=db.execute('SELECT event_id,materialize_token,tenant_id,bulk_id,item_id,asset_version_id,variants,master_artifact_id,media_type,output_role FROM photo.claim_media_retry_job()').fetchone()
    assert claimed[0]==event and claimed[2:8]==(tenant,bulk,item,version,variants,artifact)
    assert claimed[8:]==('application/json','working_image')
    token=claimed[1]
    assert db.execute('SELECT photo.renew_media_retry_job(%s,%s)',(event,token)).fetchone()[0]
    assert db.execute('SELECT photo.release_media_retry_job(%s,%s)',(event,token)).fetchone()[0]
    replay=db.execute('SELECT event_id,materialize_token FROM photo.claim_media_retry_job()').fetchone()
    assert replay[0]==event and replay[1]!=token
    token=replay[1];delivery_run=uuid4();srgb='b'*64
    config={'event_id':str(event),'asset_version_id':str(version),'variants':variants,'srgb_sha256':srgb}
    assert db.execute('SELECT photo.begin_media_retry_plan(%s,%s,%s,%s,%s)',(event,token,delivery_run,'c'*64,Jsonb(config))).fetchone()[0]
    inputs=[{'artifact_id':str(artifact),'sha256':digest}]
    for variant in ('thumb320','thumb640','preview1600','preview2560'):
        node=uuid4();operation='media.thumbnail' if variant.startswith('thumb') else 'media.preview';params_sha=('d' if variant.startswith('thumb') else 'e')*64
        plan={'id':str(node),'asset_version_id':str(version),'variant':variant,'operation':operation,'profile_hash':'9'*64,'inputs':inputs,'params_sha256':params_sha}
        assert db.execute('SELECT photo.register_media_retry_node(%s,%s,%s,%s,%s)',(event,token,delivery_run,Jsonb(plan),event)).fetchone()[0]
        reservation=db.execute('SELECT outcome,artifact_id,token,ready FROM photo.reserve_media_retry_document(%s,%s,%s,%s,%s,%s)',(tenant,delivery_run,node,params_sha,100,'photo')).fetchone()
        assert reservation[0]=='created' and not reservation[3]
        assert db.execute('SELECT photo.complete_media_retry_document(%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,delivery_run,node,reservation[1],reservation[2],params_sha,100,'photo',f'v-{variant}')).fetchone()[0]
        task={'id':str(node),'node_key':f'media.retry/{event}/{variant}','operation':operation,'operation_version':'1','resource_class':'media','timeout_seconds':180 if operation=='media.thumbnail' else 300,'params':{'artifact_id':str(reservation[1]),'sha256':params_sha},'inputs':inputs,'input_digest':'f'*64,'execution_profile':{'backend':'cpu','precision':'float32','resource_estimate':{'ram_bytes':1,'vram_bytes':0,'scratch_bytes':1,'output_bytes':1,'pixel_count':1}}}
        assert db.execute('SELECT photo.materialize_media_retry_task(%s,%s,%s,%s)',(event,token,delivery_run,Jsonb(task))).fetchone()[0]
    assert db.execute('SELECT photo.seal_media_retry_plan(%s,%s,%s)',(event,token,delivery_run)).fetchone()[0]
    assert db.execute('SELECT status,run_id,materialize_token FROM photo.media_retry_jobs WHERE event_id=%s',(event,)).fetchone()==('running',delivery_run,None)
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(delivery_run,)).fetchone()==('running',True)
    assert db.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s AND state=%s',(delivery_run,'ready')).fetchone()[0]==4
    for task_id,variant in db.execute('SELECT id,variant FROM photo.media_retry_nodes WHERE run_id=%s ORDER BY variant',(delivery_run,)).fetchall():
        derivative,manifest=uuid4(),uuid4();out_sha,manifest_sha='1'*64,'2'*64
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,%s,'photo',%s,'v1',%s,10,'image/jpeg','ready',%s,1,1,'derivative'),(%s,%s,'photo',%s,'v1',%s,10,'application/json','ready',%s,1,1,'derivative_manifest')",(derivative,tenant,f'derivatives/{derivative}',out_sha,task_id,manifest,tenant,f'derivatives/{manifest}',manifest_sha,task_id))
        outputs=[{'artifact_id':str(derivative),'sha256':out_sha},{'artifact_id':str(manifest),'sha256':manifest_sha}]
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task_id,Jsonb(outputs),'3'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s WHERE id=%s",(result,task_id))
    assert db.execute('SELECT photo.finalize_media_retry_job()').fetchone()[0]
    assert db.execute('SELECT status,error_code FROM photo.media_retry_jobs WHERE event_id=%s',(event,)).fetchone()==('completed',None)
    assert db.execute('SELECT count(*) FROM photo.derivatives WHERE tenant_id=%s AND asset_version_id=%s AND status=%s',(tenant,version,'ready')).fetchone()[0]==4
