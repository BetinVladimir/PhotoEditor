"""Authored SQL authority scenarios. No physical image qualification is credited."""
from uuid import UUID, uuid4
import pytest
from psycopg import errors
from test_v27_semantic_final_producer_gate import fixture


@pytest.mark.parametrize('role,allowed', [('photo_scheduler',True),('photo_api',False),
    ('photo_worker',False),('photo_gateway',False),('photo_portal',False)])
def test_local_result_publisher_is_scheduler_only(db,role,allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.publish_v28_local_portrait_result(uuid,uuid,uuid)','EXECUTE')",(role,)).fetchone()==(allowed,)


def test_local_result_receipt_guard_observes_actual_caller(db):
    assert db.execute("SELECT prosecdef FROM pg_proc WHERE oid='photo.guard_v28_local_result_receipt()'::regprocedure").fetchone()==(False,)


@pytest.mark.parametrize('mutation',['insert','update'])
def test_api_cannot_inject_local_result_receipt(db,mutation):
    tid,task,*_=fixture(db,snapshot_authority=True,qualification=False)
    run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]
    with pytest.raises(errors.InsufficientPrivilege):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
            db.execute('SET LOCAL ROLE photo_api')
            if mutation=='update':
                db.execute("UPDATE photo.runs SET v28_local_result_receipt='{}'::jsonb WHERE tenant_id=%s AND id=%s",(tid,run))
            else:
                db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,v28_local_result_receipt) SELECT %s,tenant_id,kind,'queued',input_snapshot_id,config_snapshot,config_hash,created_by,'{}'::jsonb FROM photo.runs WHERE tenant_id=%s AND id=%s",(uuid4(),tid,run))
    assert db.execute('SELECT v28_local_result_receipt FROM photo.runs WHERE id=%s',(run,)).fetchone()==(None,)


@pytest.mark.parametrize('function,arguments',[
    ('v28_local_portrait_result_ready','%s,%s,%s,%s'),
    ('v28_local_portrait_review_decision_ready','%s,%s,%s,%s')])
def test_missing_local_receipt_never_qualifies(db,function,arguments):
    tid,task,*tail=fixture(db,snapshot_authority=True,qualification=False)
    run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
    assert db.execute('SELECT photo.'+function+'('+arguments+')',(tid,run,uuid4(),uuid4())).fetchone()==(False,)


def adopted_local_result(db,operation='portrait.hair',review_required=False):
    """Real relational pin/source/typed closure; binary/photo checks are separate."""
    import json
    from copy import deepcopy
    from hashlib import sha256
    from psycopg.types.json import Jsonb
    from photo_worker.contracts.base import canonical_bytes
    from background_cleanup_preview_documents_fixture import working_image_builder
    from semantic_qualification_fixture import frozen_qualification
    tid,template,*tail=fixture(db,snapshot_authority=True,qualification=False,
        metadata_bodies=True,source_builder=working_image_builder(with_alpha=True))
    scope=tail[-1];actor,session,project=scope['actor'],scope['session'],scope['project']
    old_run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(template,)).fetchone()[0]
    run,task,workflow=uuid4(),uuid4(),uuid4()
    def artifact(raw,role=None,producer=None,media='application/json'):
        aid=uuid4();digest=sha256(raw).hexdigest()
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,%s,%s,%s,clock_timestamp(),%s,%s,%s)",
            (aid,tid,str(aid),digest,len(raw),media,role,producer,1 if producer else None,1 if producer else None))
        return dict(artifact_id=str(aid),sha256=digest)
    base=json.loads(scope['source_body'])
    binding=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash=sha256(canonical_bytes(base['canvas'])).hexdigest())
    mask_pixels=artifact(b'fixture target NPY',media='application/x-npy')
    protected_pixels=artifact(b'fixture protected NPY',media='application/x-npy')
    def mask(reference,protected):
        return artifact(canonical_bytes(dict(artifact=reference,canvas=base['canvas'],transform_chain=base['transform_chain'],object_id=None,selector='protected' if protected else 'manual',confidence=1.,model_lock_id=None,protected=protected)))
    mask_ref=mask(mask_pixels,False);protected_ref=mask(protected_pixels,True)
    body=dict(schema_version=1,base=binding,strength=0.5,mask=mask_ref,protected=protected_ref,hue_delta_degrees=0.,saturation_factor=1.)
    if operation=='portrait.hair':body['detail']=0.5
    if operation=='portrait.clothing':body['wrinkle_strength']=0.5 if review_required else 0
    raw=canonical_bytes(body);params=artifact(raw)
    manifest=artifact(b'{"fixture":"manifest"}');chunk=artifact(b'{"fixture":"chunk"}')
    pin=frozen_qualification(db,tid,project,actor,operation=operation,backend='cpu')
    pin.pop('calibration');pin.update(operation=operation,operation_version='1',dataset_sha='d'*64)
    pin['policy_sha']=db.execute('SELECT photo.v28_local_policy_sha(%s)',(pin['policy_id'],)).fetchone()[0]
    config=dict(project_id=str(project),v28_local_portrait=dict(auth_session_id=str(session),intent=dict(task_id=str(task),operation=operation,params_jcs=raw.decode(),params_sha=params['sha256'])))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed,v28_local_qualification_pin) SELECT %s,tenant_id,'v2',%s,input_snapshot_id,%s,%s,created_by,'fail_fast',true,%s FROM photo.runs WHERE id=%s",(run,'waiting_review' if review_required else 'completed',Jsonb(config),sha256(canonical_bytes(config)).hexdigest(),Jsonb(pin),old_run))
    db.execute('RESET ROLE')
    assert db.execute("""SELECT r.kind='v2',s.kind='v2',r.input_snapshot_id=s.id,
        r.failure_policy='fail_fast',r.config_snapshot->>'project_id'=%s
        FROM photo.runs r JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
        WHERE r.tenant_id=%s AND r.id=%s""",(str(project),tid,run)).fetchone()==(True,True,True,True,True)
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) SELECT %s,tenant_id,project_id,%s,input_snapshot_id,'preview','v2.2',policy_sha FROM photo.v2_workflows WHERE run_id=%s",(workflow,run,old_run))
    profile=dict(renderer_version='v2.2',backend='cpu',portrait_runtime_sha=pin['runtime_lock_sha'],contracts_sha='4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4',v2_entry_count=8,v2_entries_sha='1'*64,v2_input_manifest=manifest)
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,deadline) VALUES(%s,%s,%s,'local:0',%s,'1','cpu','queued',%s,%s,%s,%s,1,1,clock_timestamp()+interval '1 hour')",(task,tid,run,operation,Jsonb([manifest,params]),params['artifact_id'],'b'*64,Jsonb(profile)))
    for ordinal,(ref,contract) in enumerate([(params,'Params'),(binding['source'],'WorkingImage'),(base['pixels'],'WorkingPixels'),(base['alpha'],'WorkingAlpha'),(mask_ref,'MaskRef'),(protected_ref,'MaskRef'),(mask_pixels,'MaskPixels'),(protected_pixels,'MaskPixels'),(chunk,'InputClosureChunk'),(manifest,'InputClosureManifest')]):
        db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tid,project,task,ref['artifact_id'],ref['sha256'],contract,ordinal))
    candidate=deepcopy(base);candidate['pixels']=artifact(b'local RGB fixture','pixels',task,'application/x-npy')
    image_raw=canonical_bytes(candidate);image=artifact(image_raw,'working_image',task);root=artifact(b'{"fixture":"root"}','document',task)
    worker=uuid4()
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tid,task,worker))
    # Relational output receipts are synthetic, not successful physical writes.
    # Match the actual attempt worker and every immutable artifact locator before
    # canonical commit; the unchanged V2 root guard must validate this receipt.
    for ref in (candidate['pixels'],image,root):
        inserted=db.execute("""INSERT INTO photo.output_uploads(
            tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,
            idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,
            media_type,state,object_version,completed_at)
            SELECT tenant_id,%s,1,1,%s,%s,%s,id,output_role,bucket,object_key,
                   sha256,bytes,media_type,'ready',object_version,clock_timestamp()
            FROM photo.artifacts WHERE tenant_id=%s AND id=%s
              AND created_by_task_id=%s AND produced_attempt=1 AND produced_fence=1
            RETURNING artifact_id""",
            (task,worker,'a'*64,uuid4(),tid,ref['artifact_id'],task)).fetchone()
        assert inserted==(UUID(ref['artifact_id']),)
    assert db.execute("""SELECT count(*) FROM photo.artifacts a
        JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
        JOIN photo.task_attempts attempt ON attempt.tenant_id=u.tenant_id
          AND attempt.task_id=u.task_id AND attempt.attempt=u.attempt
          AND attempt.fencing_token=u.fence AND attempt.worker_id=u.worker_id
        WHERE a.tenant_id=%s AND a.id=%s AND a.output_role='document'
          AND a.state='ready' AND a.verified_at IS NOT NULL
          AND a.created_by_task_id=%s AND a.produced_attempt=1 AND a.produced_fence=1
          AND u.task_id=%s AND u.attempt=1 AND u.fence=1 AND u.role=a.output_role
          AND u.state='ready' AND u.completed_at IS NOT NULL
          AND u.bucket=a.bucket AND u.object_key=a.object_key
          AND u.object_version=a.object_version AND u.sha256=a.sha256
          AND u.bytes=a.bytes AND u.media_type=a.media_type""",
        (tid,root['artifact_id'],task,task)).fetchone()==(1,)
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest,review_required) VALUES(%s,%s,1,1,%s,'{}',%s,%s) RETURNING id",(tid,task,Jsonb([root]),'e'*64,review_required)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,'task_params',%s,%s,%s,%s,'fixture','ready','v1',clock_timestamp())",(tid,run,task,params['artifact_id'],uuid4(),params['sha256'],len(raw)))
        for ref,path in [(root,'$'),(image,'$/working_image'),(candidate['pixels'],'$/working_image/pixels'),(candidate['alpha'],'$/working_image/alpha')]:
            db.execute("INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,encode(sha256(convert_to(%s,'UTF8')),'hex'),0)",(tid,project,workflow,ref['artifact_id'],ref['sha256'],'tasks/'+str(task)+'/'+path))
        db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
    db.execute('RESET ROLE')
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
    assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task,)).fetchone()==(True,)
    assert db.execute('SELECT photo.v28_local_qualification_pin_ready(%s,%s,%s)',(tid,run,task)).fetchone()==(True,)
    assert db.execute('SELECT params_id FROM photo.v28_local_portrait_adoption_params(%s,%s,1,1,%s)',(tid,task,result)).fetchone()==(UUID(params['artifact_id']),)
    return dict(tenant=tid,run=run,workflow=workflow,task=task,result=result,actor=actor,session=session,project=project,
        base=scope['source_body'].decode(),image=image_raw.decode(),candidate=image,policy=pin['policy_id'])


@pytest.mark.parametrize('operation',['portrait.hair','portrait.clothing'])
def test_local_auto_result_publication_and_version_replay(db,operation):
    from uuid import uuid5
    item=adopted_local_result(db,operation)
    args=(item['tenant'],item['task'],item['result'])
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.publish_v28_local_portrait_result(%s,%s,%s)',args).fetchone()==(item['task'],)
        assert db.execute('SELECT photo.publish_v28_local_portrait_result(%s,%s,%s)',args).fetchone()==(item['task'],)
    db.execute('RESET ROLE')
    materialize=(item['tenant'],item['workflow'],item['actor'],item['session'],item['result'],item['candidate']['sha256'],item['base'],item['image'])
    expected=uuid5(item['result'],'V28.LocalPortraitAutoAppliedVersion')
    for _ in range(2):
        assert db.execute('SELECT photo.apply_v28_local_portrait_auto_version(%s,%s,%s,%s,%s,%s,%s,%s)',materialize).fetchone()==(expected,)
    assert db.execute('SELECT count(*) FROM photo.asset_versions WHERE id=%s',(expected,)).fetchone()==(1,)
    db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(item['policy'],))
    assert db.execute('SELECT photo.apply_v28_local_portrait_auto_version(%s,%s,%s,%s,%s,%s,%s,%s)',materialize).fetchone()==(None,)


@pytest.mark.parametrize('operation',['portrait.hair','portrait.clothing'])
def test_local_result_history_discovery_binds_actual_run_and_rechecks_authority(db,operation):
    item=adopted_local_result(db,operation)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.publish_v28_local_portrait_result(%s,%s,%s)',(item['tenant'],item['task'],item['result'])).fetchone()==(item['task'],)
    db.execute('RESET ROLE')
    args=(item['tenant'],item['run'],item['actor'],item['session'])
    discover=lambda:db.execute('SELECT run_id,workflow_id FROM photo.discover_v28_local_portrait_result(%s,%s,%s,%s)',args).fetchall()
    assert item['run']!=item['workflow']
    assert discover()==[(item['run'],item['workflow'])]
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(item['policy'],))
        # Authorized run remains discoverable, but no applicable local result
        # can be advertised while its current qualification is withdrawn.
        assert discover()==[(item['run'],None)]
    assert discover()==[(item['run'],item['workflow'])]
    foreign=(item['tenant'],item['run'],uuid4(),item['session'])
    assert db.execute('SELECT run_id,workflow_id FROM photo.discover_v28_local_portrait_result(%s,%s,%s,%s)',foreign).fetchall()==[]


def test_hair_review_required_result_is_refused_without_publication_mutation(db):
    # Deliberately invalid result metadata, separate from the legal automatic
    # hair result. Do not forge a review/receipt to make this positive.
    item=adopted_local_result(db,'portrait.hair',review_required=True)
    tid,task,run=item['tenant'],item['task'],item['run']
    def snapshot():
        return db.execute("""SELECT to_jsonb(r),to_jsonb(t),to_jsonb(z),
            (SELECT count(*) FROM photo.reviews WHERE tenant_id=%s),
            (SELECT count(*) FROM photo.outbox WHERE tenant_id=%s)
            FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
            JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
            WHERE r.tenant_id=%s AND r.id=%s AND t.id=%s""",(tid,tid,tid,run,task)).fetchone()
    before=snapshot()
    assert before[0]['v28_local_result_receipt'] is None
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.publish_v28_local_portrait_result(%s,%s,%s)',(tid,task,item['result'])).fetchone()==(None,)
    db.execute('RESET ROLE')
    assert snapshot()==before
    assert db.execute('SELECT count(*) FROM photo.reviews WHERE tenant_id=%s AND task_id=%s',(tid,task)).fetchone()==(0,)
