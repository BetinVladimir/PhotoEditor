"""Real tiny typed/NPY seal evidence; not worker or render qualification.

The physical verifier independently reconstructs the seal. The SQL fixture will
bind these bytes to an owned admitted task; no admission predicate is stubbed.
"""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.editor_effects import EditorEffectsSealParams,EditorExecutedBaseReceipt
from photo_worker.v2.adoption import ArtifactRef
from photo_worker.v2.editor_seal_adoption import verify_editor_effects_seal
from test_editor_seal_adoption_physical import physical_fixture


def typed_bundle(*,generated=False,trace=False):
    reader,root,scope,*_=physical_fixture(generated=generated,trace=trace)
    outputs=[]
    for aid,(raw,source,role,media) in reader.items.items():
        assert sha256(raw).hexdigest()
        if source=='output':
            outputs.append({'artifact_id':str(aid),'sha256':sha256(raw).hexdigest(),
                'role':role,'media_type':media,'bytes':len(raw)})
    assert len(outputs)==(7 if trace else 6 if generated else 5)
    assert len({row['role'] for row in outputs})==len(outputs)
    return reader,root,scope,outputs


def bind_bundle(reader,root,scope,build):
    """Reissue only actual Params/receipt IDs+hashes after owned build allocation."""
    params=EditorEffectsSealParams.model_validate_json(reader.items[scope.params.artifact_id][0])
    params=params.model_copy(update={'build_run_id':build})
    receipt=EditorExecutedBaseReceipt.model_validate_json(reader.items[root.artifact_id][0])
    receipt=receipt.model_copy(update={'build_run_id':build})
    del reader.items[scope.params.artifact_id];del reader.items[root.artifact_id]
    ref=reader.add(params);published=reader.add(receipt,'output','executed_base_receipt')
    return ArtifactRef(published.artifact_id,published.sha256),type(scope)(
        ArtifactRef(ref.artifact_id,ref.sha256),scope.profile_jcs,build)


def relational_worker_seal(db,bundle=None):
    """Owned relational execution fixture; no synthetic photographic qualification.

    Reuses an admitted original recipe/Apply catalog, but allocates a NEW live
    native seal task. No installed predicate/function/trigger is replaced.
    """
    import json
    from uuid import uuid4
    from psycopg import sql
    from psycopg.types.json import Jsonb
    from editor_source_failure_fixture import admitted_final
    from test_rtx_worker_provisioning import registration_input
    from test_processing_worker_provisioning import provision_processing
    from test_processing_worker_activation import activate
    tid,build,final,*_=(admitted_physical_base(db,bundle) if bundle is not None else admitted_final(db))
    b=db.execute('SELECT to_jsonb(b) FROM photo.editor_effect_builds b WHERE run_id=%s',(build,)).fetchone()[0]
    v=registration_input(db);v.update(tenant_id=str(tid),resource_class='cpu',ram_budget_bytes=str(8*1024**3))
    provision_processing(db,v)
    report=json.dumps(dict(schema_version=1,backend='cpu',lock_id=v['model_lock_sha'],models=[]),sort_keys=True,separators=(',',':'))
    v.update(doctor_report_jcs=report,doctor_report_file_sha=sha256(report.encode()).hexdigest(),qualified_operations_json=json.dumps({'editor.effects_seal':['cpu']}))
    activate(db,v)
    db.execute(sql.SQL('SET SESSION AUTHORIZATION {}').format(sql.Identifier(v['db_principal'])))
    db.execute('SET ROLE photo_worker')
    assert db.execute('SELECT photo.heartbeat_worker_capacity_v2(%s,%s,%s,%s,%s,%s,%s,%s)',(v['worker_id'],'cpu','cpu',['editor.effects_seal'],int(v['ram_budget_bytes']),0,int(v['scratch_budget_bytes']),v['capacity_policy_sha'])).fetchone()==(True,)
    assert db.execute("SELECT photo.report_worker_model_residency(%s,%s,'[]')",(v['worker_id'],v['capacity_policy_sha'])).fetchone()==(True,)
    db.execute('RESET SESSION AUTHORIZATION');db.execute('RESET ROLE')
    run,task=uuid4(),uuid4()
    snapshot=db.execute('SELECT input_snapshot_id FROM photo.runs WHERE id=%s',(final,)).fetchone()[0]
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,failure_policy) VALUES(%s,%s,'v2','running',%s,%s,%s,%s,true,'fail_fast')",(run,tid,snapshot,Jsonb({'project_id':b['project_id']}),'b'*64,b['actor_id']))
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'provenance','v2.2',%s)",(tid,b['project_id'],run,snapshot,b['compilation_sha']))
    def ref(name):return {'artifact_id':b[name+'_artifact_id'],'sha256':b[name+'_sha']}
    original={'asset_version_id':b['asset_version_id'],'source':{'artifact_id':b['source_artifact_id'],'sha256':b['source_sha']},'canvas_hash':b['source_canvas_hash']}
    processed={'asset_version_id':b['asset_version_id'],'source':ref('processed_source'),'canvas_hash':b['source_canvas_hash']}
    params={'schema_version':1,'build_run_id':str(build),'original_recipe':ref('frozen_recipe'),'original_source':original,'processed_source':processed,'quality':ref('quality'),'executed_style':ref('executed_style'),'compilation_sha':b['compilation_sha'],'trace_sha':'b'*64,'generative_requested':False,'generated':None,'inherited_review':False,'coverage_trace':[]}
    raw=canonical_bytes(bundle['params'] if bundle is not None else params);digest=sha256(raw).hexdigest();pid=uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,verified_at) VALUES(%s,%s,'fixture',%s,'v1',%s,%s,'application/json','ready','task_params',clock_timestamp())",(pid,tid,str(pid),digest,len(raw)))
    profile={'renderer_version':'v2.2','backend':'cpu','precision':'float32','tile_size':512,'resource_estimate':{'ram_bytes':70<<20,'vram_bytes':0,'scratch_bytes':70<<20,'output_bytes':7<<20,'pixel_count':16}}
    if bundle is not None:
        profile.update(json.loads(bundle['scope'].profile_jcs))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,worker_id,lease_until,deadline) VALUES(%s,%s,%s,'editor.effects_seal','editor.effects_seal','1','cpu','running',%s,%s,%s,%s,1,1,%s,clock_timestamp()+interval '2minutes',clock_timestamp()+interval '4minutes')",(task,tid,run,Jsonb([ref('frozen_recipe'),original['source'],processed['source'],ref('quality'),ref('executed_style')]),pid,'a'*64,Jsonb(profile),v['worker_id']))
    if bundle is not None:
        refs=[{'artifact_id':str(aid),'sha256':sha256(raw).hexdigest()} for aid,(raw,source,role,media) in bundle['reader'].items.items() if source=='input']
        db.execute('UPDATE photo.tasks SET inputs=%s WHERE id=%s',(Jsonb(refs),task))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),'cpu','{}')",(tid,task,v['worker_id']))
    db.execute("UPDATE photo.resource_slots SET tenant_id=%s,task_id=%s,fencing_token=1,lease_until=clock_timestamp()+interval '2minutes' WHERE id=(SELECT id FROM photo.resource_slots WHERE node_id=%s AND resource_class='cpu' ORDER BY id LIMIT 1)",(tid,task,v['node_id']))
    db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,editor_effects_params) VALUES(%s,%s,%s,'task_params',%s,%s,%s,%s,'fixture','ready','v1',clock_timestamp(),%s)",(tid,run,task,pid,uuid4(),digest,len(raw),raw.decode()))
    db.execute("UPDATE photo.editor_effect_builds SET state='sealing',seal_run_id=%s,seal_task_id=%s,seal_params_artifact_id=%s,seal_params_sha=%s,seal_plan_params=%s,processed_source_canvas_hash=source_canvas_hash,seal_trace_sha=%s,seal_coverage_trace=%s,seal_generative_requested=%s,seal_generated_mask=%s,seal_inherited_review=false,seal_receipt_artifact_id=NULL,seal_receipt_sha=NULL,result_artifact_id=NULL,result_sha=NULL,seal_adoption_digest=NULL,seal_adoption_sql_digest=NULL WHERE run_id=%s",(run,task,pid,digest,raw.decode(),json.loads(raw)['trace_sha'],Jsonb(json.loads(raw).get('coverage_trace',[])),json.loads(raw)['generative_requested'],Jsonb(json.loads(raw)['generated']) if json.loads(raw)['generated'] is not None else None,build))
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES(%s,'user',%s,'run',%s,%s,0,'reserved',clock_timestamp()+interval '1hour')",(tid,b['actor_id'],run,7<<20))
    db.execute('UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+%s WHERE tenant_id=%s',(7<<20,tid))
    return {'tenant':tid,'build':build,'run':run,'task':task,'worker':v,'params':pid,'params_sha':digest,'params_jcs':raw.decode(),'final':final}


def stage_relational_outputs(db,seed,bundle=None):
    """Nonempty real typed bytes for SQL result-shape tests ONLY.

    This inventory's byte reader is not yet bound to the relational input graph;
    consequently it must NOT be used to claim physical scheduler adoption.
    """
    from uuid import uuid4
    reader,root,scope,outputs=typed_bundle() if bundle is None else (bundle['reader'],bundle['root'],bundle['scope'],[{'artifact_id':str(aid),'sha256':sha256(raw).hexdigest(),'role':role,'media_type':media,'bytes':len(raw)} for aid,(raw,source,role,media) in bundle['reader'].items.items() if source=='output'])
    refs=[]
    for row in outputs:
        aid=row['artifact_id'];role=row['role']
        key='tenants/'+str(seed['tenant'])+'/runs/'+str(seed['run'])+'/outputs/'+aid
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,%s,%s,'ready',%s,clock_timestamp(),%s,1,1)",(aid,seed['tenant'],key,row['sha256'],row['bytes'],row['media_type'],role,seed['task']))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,'fixture',%s,%s,%s,%s,'ready','v1',clock_timestamp())",(seed['tenant'],seed['task'],seed['worker']['worker_id'],seed['worker']['certificate_sha'],uuid4(),aid,role,key,row['sha256'],row['bytes'],row['media_type']))
        refs.append({'artifact_id':aid,'sha256':row['sha256']})
    return refs


def full_physical_bundle(*,generated=False,trace=False):
    """One immutable graph shared by catalog and independent byte validation."""
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.contracts.quality import QualityReport
    from photo_worker.operations.optical.contracts import ImageResult,ImageReport
    reader,root,scope,*_=physical_fixture(generated=generated,trace=trace)
    params=EditorEffectsSealParams.model_validate_json(reader.items[scope.params.artifact_id][0])
    if trace:
        entries=[entry.model_copy(update={'fence':1}) for entry in params.coverage_trace]
        params=params.model_copy(update={'coverage_trace':entries,'trace_sha':canonical_hash([entry.model_dump(mode='json') for entry in entries])})
    processed=params.processed_source.source if trace else reader.add(__import__('json').loads(reader.items[params.processed_source.source.artifact_id][0]))
    binding=params.processed_source.model_copy(update={'source':processed})
    quality=QualityReport.model_validate_json(reader.items[params.quality.artifact_id][0]).model_copy(update={'target':processed})
    quality_ref=reader.add(quality)
    old_receipt=EditorExecutedBaseReceipt.model_validate_json(reader.items[root.artifact_id][0])
    old_result=ImageResult.model_validate_json(reader.items[old_receipt.image_result.artifact_id][0])
    old_report=ImageReport.model_validate_json(reader.items[old_result.report.artifact_id][0])
    for aid in [scope.params.artifact_id,root.artifact_id,old_receipt.image_result.artifact_id,old_result.report.artifact_id]:del reader.items[aid]
    report_ref=reader.add(old_report.model_copy(update={'sources':[binding]}),'output','image_report')
    result_ref=reader.add(old_result.model_copy(update={'working_image':processed,'report':report_ref}),'output','image_result')
    params=params.model_copy(update={'processed_source':binding,'quality':quality_ref})
    params_ref=reader.add(params)
    receipt_ref=reader.add(old_receipt.model_copy(update={'processed_source':binding,'quality':quality_ref,'trace_sha':params.trace_sha,'image_result':result_ref}),'output','executed_base_receipt')
    scope=type(scope)(ArtifactRef(params_ref.artifact_id,params_ref.sha256),scope.profile_jcs,params.build_run_id)
    root=ArtifactRef(receipt_ref.artifact_id,receipt_ref.sha256)
    return {'reader':reader,'root':root,'scope':scope,'params':params,'params_ref':params_ref}


from uuid import uuid4
from psycopg.types.json import Jsonb
def physical_build(db,bundle):
    count=1;params=bundle['params'];reader=bundle['reader'];master=params.original_source.source;version_id=params.original_source.asset_version_id
    user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone()
    tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
    project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'v4','restricted') RETURNING id",(tenant,)).fetchone()[0]
    source=db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES(%s,%s,'source','draft') RETURNING id",(tenant,project)).fetchone()[0]
    items=[]
    for ordinal in range(count):
        digest=master.sha256
        artifact=db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,verified_at) VALUES(%s,%s,'photo',%s,'v1',%s,%s,'application/json','ready','working_image',clock_timestamp()) RETURNING id",(master.artifact_id,tenant,f'masters/{master.artifact_id}',digest,len(reader.items[master.artifact_id][0]))).fetchone()[0]
        asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,artifact)).fetchone()[0]
        version=db.execute("INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,%s,'{}','accepted') RETURNING id",(version_id,tenant,asset,artifact,digest)).fetchone()[0]
        item=db.execute("INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES(%s,%s,%s,%s,%s) RETURNING id",(tenant,source,asset,version,ordinal+1)).fetchone()[0]
        items.append((item,asset,version,artifact,digest))
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'selection',%s,1,'user',%s,%s,%s) RETURNING id",(tenant,source,user,'a'*64,count)).fetchone()[0]
    for ordinal,(item,_,version,artifact,digest) in enumerate(items):
        db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,%s,%s,%s,%s,%s,%s)",(tenant,snapshot,ordinal,item,version,artifact,digest,Jsonb({'position':ordinal+1})))
    run=db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,%s,'apply','queued',%s,'{}',%s,%s,false) RETURNING id",(params.build_run_id,tenant,snapshot,'b'*64,user)).fetchone()[0]
    token=uuid4()
    db.execute("INSERT INTO photo.apply_builds(tenant_id,run_id,source_collection_id,item_count,phase,graph_version,lease_token,lease_until) VALUES(%s,%s,%s,%s,'resolve',4,%s,clock_timestamp()+interval '2 minutes')",(tenant,run,source,count,token))
    return tenant,run,token,items



def admitted_physical_base(db,bundle):
    native_seal=False;physical=bundle['params'];reader=bundle['reader']
    tid, origin, _, items = physical_build(db,bundle)
    _, _, version, source, source_sha = items[0]
    actor, snapshot, project = db.execute("SELECT r.created_by,r.input_snapshot_id,c.project_id FROM photo.runs r JOIN photo.apply_builds b ON b.run_id=r.id JOIN photo.collections c ON c.id=b.source_collection_id WHERE r.id=%s", (origin,)).fetchone()
    session = db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id", (actor,uuid4().bytes)).fetchone()[0]
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute("UPDATE photo.artifacts SET verified_at=clock_timestamp() WHERE id=%s", (source,))
    db.execute("UPDATE photo.apply_builds SET source_collection_id=NULL WHERE run_id=%s", (origin,))
    db.execute("UPDATE photo.runs SET status='completed',graph_sealed=true WHERE id=%s", (origin,))

    mapped={'document':physical.original_recipe,'task_params':bundle['params_ref'],
        'generation_report':physical.coverage_trace[-1].report if physical.coverage_trace else None,
        'working_image':physical.processed_source.source,'quality_report':physical.quality,'resolved_style':physical.executed_style}
    inserted={physical.original_source.source.artifact_id}
    def store(ref,role=None,producer=None):
        aid=ref.artifact_id;raw,_,_,media=reader.items[aid]
        if aid not in inserted:
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,%s,%s,'ready',%s,clock_timestamp(),%s,%s,%s)",(aid,tid,str(aid),ref.sha256,len(raw),media,role,producer,1 if producer else None,1 if producer else None))
            inserted.add(aid)
        return {'artifact_id':str(aid),'sha256':ref.sha256}
    def artifact(role,producer=None):
        if role in mapped:return store(mapped[role],role,producer)
        aid=uuid4();sha=aid.hex*2
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,2,'application/json','ready',%s,clock_timestamp(),%s,%s,%s)",(aid,tid,str(aid),sha,role,producer,1 if producer else None,1 if producer else None))
        return {'artifact_id':str(aid),'sha256':sha}
    # Store real reachable pixel/profile/style dependencies, leaving canonical
    # producer/quality outputs to their actual task rows below.
    held={v.artifact_id for v in mapped.values() if v is not None}
    for aid,(raw,origin_kind,role,media) in reader.items.items():
        if origin_kind=='input' and aid not in held:store(ArtifactRef(aid,sha256(raw).hexdigest()))

    recipe=artifact('document'); params=artifact('task_params')
    frozen,draft=uuid4(),uuid4()
    stream=db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",(tid,project,version)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,%s,1,'EditingRecipeV3',%s,%s)",(draft,tid,project,stream,recipe['artifact_id'],recipe['sha256']))
    db.execute('UPDATE photo.v2_document_streams SET current_document_id=%s WHERE id=%s',(draft,stream))
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,render_registered_at,render_source_id,render_source_sha,render_draft_document_id,render_draft_revision) VALUES(%s,%s,%s,%s,%s,%s,%s,'fixture','ready','v1',clock_timestamp(),clock_timestamp(),%s,%s,%s,1)", (tid,frozen,project,version,recipe['artifact_id'],recipe['sha256'],len(reader.items[physical.original_recipe.artifact_id][0]),source,source_sha,draft))

    stage_snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) SELECT tenant_id,'v2','user',%s,digest,item_count,clock_timestamp()+interval '1hour' FROM photo.snapshots WHERE id=%s RETURNING id",(actor,snapshot)).fetchone()[0]
    db.execute('INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) SELECT tenant_id,%s,ordinal,item_id,asset_version_id,artifact_id,sha256,payload FROM photo.snapshot_items WHERE snapshot_id=%s',(stage_snapshot,snapshot))
    db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(stage_snapshot,))
    def run(state,kind="apply"):
        return db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,failure_policy) VALUES(%s,%s,%s,%s,%s,%s,%s,true,'fail_fast') RETURNING id", (tid,kind,state,stage_snapshot,Jsonb({'project_id':str(project)}),'b'*64,actor)).fetchone()[0]

    sealed, final = run('completed','v2' if native_seal else 'apply'), run('running','v2')
    for stage,workflow_type in ([(sealed,'provenance')] if native_seal else [])+[(final,'preview')]:
        db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'v2.2',%s)",(tid,project,stage,stage_snapshot,workflow_type,'b'*64))
    profile=Jsonb({'renderer_version':'v2.2','backend':'cpu','precision':'float32','tile_size':512,'resource_estimate':{'ram_bytes':1,'vram_bytes':0,'scratch_bytes':1,'output_bytes':1,'pixel_count':1}})

    def task(owner, operation, roles, ready=False):
        task_id,result_id=(physical.coverage_trace[-1].task_id,physical.coverage_trace[-1].result_id) if physical.coverage_trace and operation==physical.coverage_trace[-1].operation else (uuid4(),uuid4())
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,canonical_result_id) VALUES(%s,%s,%s,%s,%s,'1','cpu',%s,'[]',%s,%s,%s,%s,%s,%s)", (task_id,tid,owner,str(task_id),operation,'ready' if ready else 'succeeded',params['artifact_id'],'a'*64,profile,0 if ready else 1,0 if ready else 1,None if ready else result_id))
        outputs=[artifact(role,task_id) for role in roles]
        if not ready:
            db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')", (tid,task_id,uuid4()))
            db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)", (result_id,tid,task_id,Jsonb(outputs),'e'*64))
        return task_id,result_id,outputs

    producer,producer_result,processed=task(origin,physical.coverage_trace[-1].operation if physical.coverage_trace else 'image.color',['working_image','generation_report'] if physical.coverage_trace else ['working_image'])
    if physical.coverage_trace:
        entry=physical.coverage_trace[-1];item,asset,version,_,_=items[0]
        db.execute("UPDATE photo.tasks SET inputs=%s WHERE id=%s",(Jsonb([entry.source.model_dump(mode='json')]),producer))
        db.execute("INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,params_sha256,params) VALUES(%s,%s,%s,'local',0,%s,%s,%s,%s,%s,%s,%s,%s)",(producer,tid,origin,item,asset,version,str(producer),entry.operation,Jsonb([entry.source.model_dump(mode='json')]),params['sha256'],Jsonb({'image':entry.source.model_dump(mode='json')})))
    quality,quality_result,quality_refs=task(origin,'quality.check',['quality_report'])
    seal,_,seal_refs=task(sealed,'editor.effects_seal',['executed_base_receipt','image_result'])
    pending,_,_=task(final,'render.materialize',[],ready=True)
    style=artifact('resolved_style')
    with db.transaction():
        # Test database owner seeds post-adoption rows; fn_owner intentionally has no INSERT grant.
        db.execute("INSERT INTO photo.editor_effect_builds(tenant_id,run_id,project_id,actor_id,session_id,asset_version_id,source_artifact_id,source_sha,source_canvas_hash,frozen_recipe_artifact_id,frozen_recipe_sha,frozen_document_id,draft_document_id,draft_revision,compilation_sha,state,seal_adoption_digest,seal_adoption_sql_digest,seal_run_id,seal_task_id,seal_receipt_artifact_id,seal_receipt_sha,result_artifact_id,result_sha,processed_source_artifact_id,processed_source_sha,executed_style_artifact_id,executed_style_sha,quality_artifact_id,quality_sha,producer_task_id,producer_result_id,producer_attempt,producer_fence,quality_task_id,quality_result_id,final_run_id) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,1,%s,'composing',%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,1,1,%s,%s,%s)", (tid,origin,project,actor,session,version,source,source_sha,physical.original_source.canvas_hash,recipe['artifact_id'],recipe['sha256'],frozen,draft,physical.compilation_sha,'c'*64,'d'*64,sealed,seal,*seal_refs[0].values(),*seal_refs[1].values(),*processed[0].values(),*style.values(),*quality_refs[0].values(),producer,producer_result,quality,quality_result,final))
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



class CatalogReader:
    """Production scheduler evidence reader over the SAME real byte graph."""
    def __init__(self,db,seed,bundle):
        self.db=db;self.seed=seed;self.items=dict(bundle['reader'].items)
        self.items[seed['params']]=(seed['params_jcs'].encode(),'input',None,'application/json')
    def evidence(self,ref):
        from photo_worker.v2.adoption import ArtifactEvidence
        s=self.seed
        row=self.db.execute('SELECT * FROM photo.editor_effect_seal_adoption_artifact(%s,%s,1,1,%s)',(s['tenant'],s['task'],ref.artifact_id)).fetchone()
        if row is None:raise ValueError('SQL_EVIDENCE_NOT_CURRENT')
        bucket,key,version,digest,size,media,role,source=row
        if ref.sha256!=digest:raise ValueError('SQL_EVIDENCE_HASH')
        return ArtifactEvidence(ref,media,size,role,source)
    def _raw(self,ref,limit):
        evidence=self.evidence(ref);raw=self.items[ref.artifact_id][0]
        if len(raw)!=evidence.bytes or len(raw)>limit or sha256(raw).hexdigest()!=ref.sha256:raise ValueError('PHYSICAL_BYTES_NOT_SQL_EVIDENCE')
        return raw
    def read_json(self,ref,limit):return self._raw(ref,limit)
    def iter_bytes(self,ref,limit):
        raw=self._raw(ref,limit)
        for offset in range(0,len(raw),4096):yield raw[offset:offset+4096]
    def staged_outputs(self):
        s=self.seed
        rows=self.db.execute('SELECT * FROM photo.editor_effect_seal_adoption_staged_outputs(%s,%s,1,1)',(s['tenant'],s['task'])).fetchall()
        if any(row[2]!='ready' for row in rows):raise ValueError('SQL_OUTPUT_NOT_READY')
        return {ArtifactRef(row[0],row[1]) for row in rows}
