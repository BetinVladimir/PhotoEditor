"""Owner-seeded admitted relational inventory; all production gates stay intact.

No physical photo/GPU qualification is implied. Immutable descriptors are seeded
once, canonical task attempts/results are complete, and deferred constraints are
checked before returning. This is a post-adoption inventory, not an admission
API or worker execution test.
"""
from uuid import uuid4
from psycopg.types.json import Jsonb
from test_apply_v4_plan import _build


def editor_inventory(db,admit=None):
    tid, origin, _, items = _build(db, count=1)
    _, _, version, source, source_sha = items[0]
    actor, snapshot, project = db.execute("SELECT r.created_by,r.input_snapshot_id,c.project_id FROM photo.runs r JOIN photo.apply_builds b ON b.run_id=r.id JOIN photo.collections c ON c.id=b.source_collection_id WHERE r.id=%s", (origin,)).fetchone()
    session = db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id", (actor,uuid4().bytes)).fetchone()[0]
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute("UPDATE photo.artifacts SET verified_at=clock_timestamp() WHERE id=%s", (source,))
    db.execute("UPDATE photo.apply_builds SET source_collection_id=NULL WHERE run_id=%s", (origin,))
    db.execute("UPDATE photo.runs SET status='completed',graph_sealed=true WHERE id=%s", (origin,))

    def artifact(role, producer=None):
        aid=uuid4(); sha=aid.hex*2
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,2,'application/json','ready',%s,clock_timestamp(),%s,%s,%s)", (aid,tid,str(aid),sha,role,producer,1 if producer else None,1 if producer else None))
        return dict(artifact_id=str(aid),sha256=sha)

    recipe=artifact('document'); params=artifact('task_params')
    frozen,draft=uuid4(),uuid4()
    stream=db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",(tid,project,version)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,%s,1,'EditingRecipeV3',%s,%s)",(draft,tid,project,stream,recipe['artifact_id'],recipe['sha256']))
    db.execute('UPDATE photo.v2_document_streams SET current_document_id=%s WHERE id=%s',(draft,stream))
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,render_registered_at,render_source_id,render_source_sha,render_draft_document_id,render_draft_revision) VALUES(%s,%s,%s,%s,%s,%s,2,'fixture','ready','v1',clock_timestamp(),clock_timestamp(),%s,%s,%s,1)", (tid,frozen,project,version,recipe['artifact_id'],recipe['sha256'],source,source_sha,draft))

    style=artifact('resolved_style')
    db.execute("UPDATE photo.runs SET status='queued',graph_sealed=false WHERE id=%s",(origin,))
    db.execute("INSERT INTO photo.editor_effect_builds(tenant_id,run_id,project_id,actor_id,session_id,asset_version_id,source_artifact_id,source_sha,source_canvas_hash,frozen_recipe_artifact_id,frozen_recipe_sha,frozen_document_id,draft_document_id,draft_revision,compilation_sha,executed_style_artifact_id,executed_style_sha) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,1,%s,%s,%s)",(tid,origin,project,actor,session,version,source,source_sha,'a'*64,recipe['artifact_id'],recipe['sha256'],frozen,draft,'b'*64,*style.values()))
    if admit:admit(db,tid,origin,actor,session,project)
    db.execute("UPDATE photo.runs SET status='completed',graph_sealed=true WHERE id=%s",(origin,))
    def run(state,kind='apply'):
        sid=snapshot
        if kind=='v2':
            sid=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,repeat('d',64),0,NULL) RETURNING id",(tid,actor)).fetchone()[0]
        return db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,failure_policy) VALUES(%s,%s,%s,%s,%s,%s,%s,true,'fail_fast') RETURNING id", (tid,kind,state,sid,Jsonb({'project_id':str(project)}),'b'*64,actor)).fetchone()[0]

    sealed, final = run('completed'), run('running',kind='v2')
    profile=Jsonb({'backend':'cpu','precision':'float32','tile_size':512,'resource_estimate':{'ram_bytes':1,'vram_bytes':0,'scratch_bytes':1,'output_bytes':1,'pixel_count':1}})

    def task(owner, operation, roles, ready=False):
        task_id,result_id=uuid4(),uuid4()
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,canonical_result_id) VALUES(%s,%s,%s,%s,%s,'1','cpu',%s,'[]',%s,%s,%s,%s,%s,%s)", (task_id,tid,owner,str(task_id),operation,'ready' if ready else 'succeeded',params['artifact_id'],'a'*64,profile,0 if ready else 1,0 if ready else 1,None if ready else result_id))
        outputs=[artifact(role,task_id) for role in roles]
        if not ready:
            db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')", (tid,task_id,uuid4()))
            db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)", (result_id,tid,task_id,Jsonb(outputs),'e'*64))
        return task_id,result_id,outputs

    producer,producer_result,processed=task(origin,'image.color',['working_image'])
    quality,quality_result,quality_refs=task(origin,'quality.check',['quality_report'])
    seal,_,seal_refs=task(sealed,'editor.effects_seal',['executed_base_receipt','image_result'])
    pending,_,_=task(final,'render.materialize',[],ready=True)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("UPDATE photo.editor_effect_builds SET (tenant_id,run_id,project_id,actor_id,session_id,asset_version_id,source_artifact_id,source_sha,source_canvas_hash,frozen_recipe_artifact_id,frozen_recipe_sha,frozen_document_id,draft_document_id,draft_revision,compilation_sha,state,seal_adoption_digest,seal_adoption_sql_digest,seal_run_id,seal_task_id,seal_receipt_artifact_id,seal_receipt_sha,result_artifact_id,result_sha,processed_source_artifact_id,processed_source_sha,executed_style_artifact_id,executed_style_sha,quality_artifact_id,quality_sha,producer_task_id,producer_result_id,producer_attempt,producer_fence,quality_task_id,quality_result_id,final_run_id)=(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,1,%s,'composing',%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,1,1,%s,%s,%s) WHERE tenant_id=%s AND run_id=%s", (tid,origin,project,actor,session,version,source,source_sha,'a'*64,recipe['artifact_id'],recipe['sha256'],frozen,draft,'b'*64,'c'*64,'d'*64,sealed,seal,*seal_refs[0].values(),*seal_refs[1].values(),*processed[0].values(),*style.values(),*quality_refs[0].values(),producer,producer_result,quality,quality_result,final,tid,origin))
    db.execute('RESET ROLE')
    for holder,size in [(origin,128),(sealed,256),(final,512)]:
        db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES(%s,'user',%s,'run',%s,%s,0,'reserved',clock_timestamp()+interval '1 hour')", (tid,actor,holder,size))
    db.execute("INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING", (tid,))
    db.execute("UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+896 WHERE tenant_id=%s", (tid,))
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')
    db.execute('SET CONSTRAINTS ALL DEFERRED')
    assert db.execute('SELECT photo.editor_effect_final_origin_ready(%s,%s)', (tid,origin)).fetchone()[0] is True
    assert db.execute('SELECT count(*) FROM photo.editor_effect_final_artifact(%s,%s,%s,%s)', (tid,origin,*seal_refs[0].values())).fetchone()[0] == 1
    descriptor=db.execute('SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.artifacts WHERE id=%s', (seal_refs[0]['artifact_id'],)).fetchone()
    return tid,origin,final,pending,descriptor,version,source,source_sha
