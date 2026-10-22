"""Real persisted prepare/source authority for the physical failure transition.

Authored, not executed. No predicate replacement: fixture first proves the live
prepare and child-source accessors accept its complete retained inventory.
"""
import json
import os
from hashlib import sha256
from uuid import uuid4
import pytest
import psycopg
from psycopg.types.json import Jsonb
from test_database import tenant
from test_delivery_manifest_download_lifecycle import persisted_document_output, seal_delivery_test_closure


def jcs(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'))


def prepared_delivery(db, presentation=False, required_signing=False):
    tid, actor = tenant(db, str(uuid4())+'@delivery-physical.test')
    # Real tenant creation owns this ledger row; the minimal tenant helper does not.
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tid,))
    session = db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,%s,'native',clock_timestamp()+interval '1 hour') RETURNING id", (actor, uuid4().bytes+uuid4().bytes, uuid4())).fetchone()[0]
    project = db.execute("INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES(%s,'Physical','workspace',true) RETURNING id", (tid,)).fetchone()[0]
    snapshot_id = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,1,clock_timestamp()+interval '1 hour') RETURNING id", (tid, actor, 'a'*64)).fetchone()[0]
    x = dict(tenant=tid, actor=actor, session=session, project=project, snapshot=snapshot_id)
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))

    def artifact(raw, role='document', media='application/json', document_run=None):
        digest = sha256(raw.encode()).hexdigest()
        aid = uuid4()
        key = f'tenants/{tid}/runs/{document_run}/documents/{aid}' if document_run else str(aid)
        aid = db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(%s,%s,'photo',%s,'source-v1',%s,%s,%s,'ready',clock_timestamp(),%s) RETURNING id", (aid,tid,key,digest,len(raw.encode()),media,role)).fetchone()[0]
        return {'artifact_id': str(aid), 'sha256': digest}

    original = artifact('{}', None)
    source = artifact('{"schema_version":1}', 'working_image')
    recipe = artifact('{"schema_version":1,"recipe":{}}')
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id", (tid, x['project'], original['artifact_id'])).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id", (tid, asset, source['artifact_id'], source['sha256'])).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')", (tid, x['snapshot'], version, source['artifact_id'], source['sha256']))
    db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot_id,))
    parent = db.execute("INSERT INTO photo.runs(failure_policy,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES('fail_fast',%s,'v2','running',%s,jsonb_build_object('project_id',%s::text),%s,%s,false) RETURNING id", (tid, snapshot_id, str(project), 'b'*64, actor)).fetchone()[0]
    workflow = db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'delivery','v2.2',%s) RETURNING id", (tid, project, parent, snapshot_id, 'c'*64)).fetchone()[0]
    prepare = db.execute("SELECT photo.v30_uuid5(%s,'v2.delivery.prepare:0')", (workflow,)).fetchone()[0]
    x['run'] = parent
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,render_source_id,render_source_sha,render_registered_at) SELECT %s,%s,%s,%s,id,sha256,bytes,bucket,'ready',object_version,clock_timestamp(),%s,%s,clock_timestamp() FROM photo.artifacts WHERE id=%s", (tid, uuid4(), x['project'], version, source['artifact_id'], source['sha256'], recipe['artifact_id']))
    preset = uuid4()
    config_raw = jcs({'metadata_policy': 'strip', **({'size': {'width': 100, 'height': 100}} if presentation else {})})
    config = artifact(config_raw)
    snapshot_raw = jcs({'schema_version': 1, 'snapshot_id': str(x['snapshot']), 'snapshot_digest': 'a'*64, 'items': [{'ordinal': 0, 'binding': {'asset_version_id': str(version), 'source': source}, 'recipe': recipe}]})
    snapshot = artifact(snapshot_raw)
    signing_policy=uuid4() if required_signing else None
    frozen_policy={'metadata_policy':'strip','provenance':'required','signing_policy_id':str(signing_policy),'signing_policy_sha':'6'*64} if required_signing else {'metadata_policy':'strip','provenance':'off'}
    frozen_raw = jcs({'schema_version': 1, 'snapshot': snapshot, 'snapshot_digest': 'a'*64, 'policy': frozen_policy, 'presets': [{'preset_id': str(preset), 'preset_sha': config['sha256'], 'required': True, 'config': config}]})
    frozen = artifact(frozen_raw)
    params_raw = jcs({'schema_version': 1, 'input': frozen})
    proof = {'schema_version': 1, 'project_id': str(x['project']), 'auth_session_id': str(x['session']), 'frozen_input_jcs': frozen_raw, 'snapshot_jcs': snapshot_raw, 'configs_jcs': [{'preset_id': str(preset), 'config_jcs': config_raw}]}
    params_ref = artifact(params_raw, document_run=parent)
    params = params_ref['artifact_id']
    policy = artifact(jcs(frozen_policy))
    x['package'] = db.execute("INSERT INTO photo.delivery_packages(tenant_id,project_id,workflow_id,snapshot_id,name,policy_artifact_id,state) VALUES(%s,%s,%s,%s,'Physical',%s,'building') RETURNING id", (tid, project, workflow, snapshot_id, policy['artifact_id'])).fetchone()[0]
    if required_signing:
        signing_artifact=db.execute("SELECT photo.v30_uuid5(%s,%s)",(tid,'signing:policy:'+str(signing_policy)+':'+'6'*64)).fetchone()[0]
        key=f'tenants/{tid}/runs/{parent}/documents/{signing_artifact}'
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(%s,%s,'photo',%s,'policy-v1',%s,2,'application/json','ready',clock_timestamp(),'v31_signing_policy')",(signing_artifact,tid,key,'6'*64))
        db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,'v31_signing_policy',%s,%s,%s,2,'photo','ready','policy-v1',clock_timestamp())",(tid,parent,signing_artifact,signing_artifact,uuid4(),'6'*64))
        closure=jcs({'tenant_id':str(tid),'actor_id':str(actor),'parent_id':str(parent),'policy_id':str(signing_policy),'policy_sha':'6'*64,'entries':[]})
        db.execute("INSERT INTO photo.delivery_signing_closures(tenant_id,project_id,package_id,parent_run_id,actor_id,policy_id,policy_sha,closure_sha,closure_jcs) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s)",(tid,project,x['package'],parent,actor,signing_policy,'6'*64,sha256(closure.encode()).hexdigest(),closure))
        x.update(signing_policy=signing_policy,signing_artifact=signing_artifact)
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'prepare','v2.delivery.prepare','1','cpu','blocked','[]',%s,%s,'{}')", (prepare, tid, parent, params, 'e'*64))
    db.execute('SET LOCAL ROLE photo_fn_owner')
    db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,v30_delivery_proof) VALUES(%s,%s,%s,'task_params',%s,%s,%s,%s,'photo','ready','source-v1',clock_timestamp(),%s)", (tid, parent, prepare, params, uuid4(), params_ref['sha256'], len(params_raw), Jsonb(proof)))
    db.execute('RESET ROLE')
    # Inputs are inserted before sealing: the production immutable-input trigger
    # remains installed and is never bypassed in this fixture.
    refs = [(params_ref, 'Params'), (frozen, 'FrozenDeliveryInput'), (snapshot, 'InputSnapshot'), (config, 'ExportPreset'), (recipe, 'FrozenRenderRecipe'), (source, 'WorkingImage')]
    for ordinal, (ref, contract) in enumerate(refs):
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)", (tid, x['project'], prepare, ref['artifact_id'], ref['sha256'], contract, ordinal))
    seal_delivery_test_closure(db,tid,project,prepare)
    estimate = {'ram_bytes': 1073741824, 'scratch_bytes': 536870912, 'vram_bytes': 0, 'pixel_count': 0, 'output_bytes': 8388608}
    result = uuid4()
    db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s,execution_profile=execution_profile||%s WHERE id=%s", (result, Jsonb({'resource_estimate': estimate}), prepare))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')", (tid, prepare, uuid4()))
    body=jcs({'schema_version':1,'input':frozen,'snapshot_digest':'a'*64,'item_count':1,'slot_count':1})
    root = persisted_document_output(db, tid, prepare, body=body)
    db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)", (result, tid, prepare, Jsonb([root]), 'f'*64))
    # Exercise the real prepare adoption barrier: it creates items/expansion
    # and deliberately unseals the parent; no synthetic inbox substitute.
    db.execute("UPDATE photo.runs SET graph_sealed=true WHERE id=%s",(parent,))
    inventory=[{**root,'purpose':sha256(b'DeliveryPreparation').hexdigest(),'ordinal':0}]
    if os.environ.get('PHOTO_TEST_DELIVERY_ADOPTION_DIAGNOSTIC')=='1':
        facts={}
        facts['source_ready']=db.execute('SELECT photo.v30_delivery_prepare_source_ready(%s,%s,%s,%s,%s,%s)',(tid,parent,prepare,params,params_ref['sha256'],params_raw)).fetchone()[0]
        facts['closure_ready']=db.execute('SELECT photo.v2_input_closure_ready(%s)',(prepare,)).fetchone()[0]
        # Mirror each dedicated barrier predicate as read-only facts. This is
        # diagnostic evidence only; the real adopter below remains mandatory.
        dedicated=db.execute("""SELECT jsonb_build_object(
          'tenant_active',EXISTS(SELECT 1 FROM photo.tenants WHERE id=t.tenant_id AND status='active'),
          'usage_exists',EXISTS(SELECT 1 FROM photo.tenant_usage WHERE tenant_id=t.tenant_id),
          'run_ready',r.kind='v2' AND r.status='running' AND NOT r.cancel_requested,
          'parent_sealed',r.graph_sealed,
          'task_ready',t.operation='v2.delivery.prepare' AND t.operation_version='1' AND t.resource_class='cpu'
            AND t.state='succeeded' AND t.attempt=1 AND t.fencing_token=1 AND t.canonical_result_id=%s AND NOT t.cancel_requested,
          'workflow_ready',w.workflow_type='delivery' AND w.renderer_version='v2.2',
          'package_ready',d.state='building' AND d.project_id=w.project_id AND d.snapshot_id=w.input_snapshot_id,
          'backend_proof_exact',b.v30_delivery_proof IS NOT NULL AND b.sha256=%s AND b.bytes=%s AND b.state='ready',
          'counts_ready',jsonb_array_length((b.v30_delivery_proof->>'snapshot_jcs')::jsonb->'items')=1
            AND jsonb_array_length((b.v30_delivery_proof->>'frozen_input_jcs')::jsonb->'presets')=1,
          'body_expected',%s::jsonb=jsonb_build_object('schema_version',1,'input',%s::jsonb->'input',
            'snapshot_digest',(b.v30_delivery_proof->>'frozen_input_jcs')::jsonb->>'snapshot_digest','item_count',1,'slot_count',1),
          'canonical_output_exact',EXISTS(SELECT 1 FROM photo.task_results z JOIN photo.artifacts a
            ON a.tenant_id=z.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid
            WHERE z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
              AND a.sha256=z.outputs->0->>'sha256' AND a.sha256=%s AND a.bytes=%s
              AND a.media_type='application/json' AND a.output_role='document'),
          'single_task_no_edges', (SELECT count(*) FROM photo.tasks WHERE tenant_id=t.tenant_id AND run_id=r.id)=1
            AND NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=t.tenant_id AND run_id=r.id),
          'no_existing_expansion_items',NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=t.tenant_id AND run_id=r.id)
            AND NOT EXISTS(SELECT 1 FROM photo.delivery_items WHERE tenant_id=t.tenant_id AND package_id=d.id))
        FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
        LEFT JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=r.id
        LEFT JOIN photo.delivery_packages d ON d.tenant_id=t.tenant_id AND d.workflow_id=w.id
        LEFT JOIN photo.backend_documents b ON b.tenant_id=t.tenant_id AND b.run_id=r.id AND b.owner_id=t.id
          AND b.purpose='task_params' AND b.artifact_id=t.params_artifact_id
        WHERE t.tenant_id=%s AND t.id=%s""",(result,params_ref['sha256'],len(params_raw.encode()),body,params_raw,
          sha256(body.encode()).hexdigest(),len(body.encode()),tid,prepare)).fetchone()[0]
        facts.update(dedicated)
        facts['root_body_exact']=db.execute("SELECT sha256=%s AND bytes=%s FROM photo.artifacts WHERE id=%s",(sha256(body.encode()).hexdigest(),len(body.encode()),root['artifact_id'])).fetchone()[0]
        facts['params_exact']=db.execute("SELECT sha256=%s AND bytes=%s FROM photo.artifacts WHERE id=%s",(sha256(params_raw.encode()).hexdigest(),len(params_raw.encode()),params)).fetchone()[0]
        with db.transaction(force_rollback=True):
            facts['generic_adoption']=db.execute('SELECT photo.adopt_v2_plan_pre_delivery(%s,%s,1,1,%s,%s,%s)',(tid,prepare,result,Jsonb(inventory),sha256(jcs(inventory).encode()).hexdigest())).fetchone()[0]
        print('DELIVERY_ADOPTION_FACTS',jcs(facts),flush=True)
        assert all(facts.values()),facts
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.adopt_v30_delivery_prepared(%s,%s,1,1,%s,%s,%s,%s,%s)',(tid,prepare,result,Jsonb(inventory),sha256(jcs(inventory).encode()).hexdigest(),params_raw,body)).fetchone()[0] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s',(parent,)).fetchone()[0] is False
    expansion=db.execute("SELECT id FROM photo.graph_expansions WHERE tenant_id=%s AND run_id=%s AND kind='v2.delivery/1'",(tid,parent)).fetchone()[0]
    token,instance=uuid4(),uuid4()
    db.execute("UPDATE photo.graph_expansions SET expansion_token=%s,expansion_lease_until=clock_timestamp()+interval '1 minute' WHERE id=%s",(token,expansion))
    # This assertion is intentionally against the current full source predicate;
    # fixture deficiencies cannot turn a refusal-only test into a false positive.
    assert db.execute('SELECT photo.v30_delivery_expansion_ready(%s)', (expansion,)).fetchone()[0] is True
    request = db.execute('SELECT photo.v30_delivery_child_request(%s,%s,0)', (expansion, token)).fetchone()[0]
    assert request is not None
    page = db.execute('SELECT photo.v30_delivery_child_sources(%s,%s,0,%s,-1)', (expansion, token, jcs(request))).fetchone()[0]
    assert any(entry['ref'] == source for entry in page['items'])
    db.execute('SELECT photo.announce_v30_delivery_runtime(%s,\'photo\')', (instance,))
    return {**x, 'prepare': prepare, 'result': result, 'expansion': expansion, 'token': token, 'instance': instance, 'source': source, 'request': jcs(request)}


def fail_source(db, x, **overrides):
    values = dict(instance=x['instance'], expansion=x['expansion'], token=x['token'], artifact=x['source']['artifact_id'], sha=x['source']['sha256'], version='source-v1', code='SOURCE_OBJECT_MISSING', slot=0, request=x['request'])
    values.update(overrides)
    db.execute('SET LOCAL ROLE photo_scheduler')
    outcome = db.execute("SELECT photo.fail_v30_delivery_physical_source(%(instance)s,'photo',%(expansion)s,%(token)s,%(artifact)s,%(sha)s,%(version)s,%(code)s,%(slot)s,%(request)s,false)", values).fetchone()[0]
    db.execute('RESET ROLE')
    return outcome


def test_delivery_physical_missing_object_fails_real_prepared_package(db):
    x = prepared_delivery(db)
    child = db.execute("SELECT photo.v30_uuid5(%s,'child:export:0')", (x['run'],)).fetchone()[0]
    cfg = {'project_id': str(x['project']), 'v30_delivery_export': json.loads(x['request'])}
    db.execute("INSERT INTO photo.runs(failure_policy,id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES('fail_fast',%s,%s,'v2','queued',%s,%s,%s,%s,true)", (child, x['tenant'], x['snapshot'], Jsonb(cfg), '9'*64, x['actor']))
    assert fail_source(db, x) is True
    assert db.execute('SELECT cancel_requested,status FROM photo.runs WHERE id=%s', (child,)).fetchone() == (True, 'queued')
    diagnostic = db.execute("SELECT payload->'source_failure' FROM photo.run_events WHERE run_id=%s AND type='v2.run.failed'", (x['run'],)).fetchone()[0]
    assert diagnostic['artifact_id'] == x['source']['artifact_id']
    assert diagnostic['sha256'] == x['source']['sha256']
    assert diagnostic['object_version'] == 'source-v1'
    assert db.execute('SELECT state FROM photo.delivery_packages WHERE id=%s', (x['package'],)).fetchone()[0] == 'failed'
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s', (x['run'],)).fetchone() == ('failed', True)
    assert db.execute('SELECT reason FROM photo.delivery_items WHERE package_id=%s', (x['package'],)).fetchone()[0] == 'SOURCE_OBJECT_MISSING'
    assert fail_source(db, x) is False


@pytest.mark.parametrize('change', ['sha', 'version', 'artifact', 'slot', 'token', 'canonical', 'session', 'lease', 'runtime', 'null_sha', 'empty_request', 'foreign_instance'])
def test_delivery_physical_failure_rechecks_current_source_authority(db, change):
    x = prepared_delivery(db)
    args = {}
    if change == 'null_sha': args['sha'] = None
    elif change == 'empty_request': args['request'] = '{}'
    elif change == 'foreign_instance': args['instance'] = uuid4()
    elif change == 'sha': args['sha'] = '0'*64
    elif change == 'version': args['version'] = 'replaced-v2'
    elif change == 'artifact': args['artifact'] = uuid4()
    elif change == 'slot': args['slot'] = 1
    elif change == 'token': args['token'] = uuid4()
    elif change == 'canonical': db.execute("UPDATE photo.tasks SET state='blocked',canonical_result_id=NULL WHERE id=%s", (x['prepare'],))
    elif change == 'session': db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (x['session'],))
    elif change == 'lease': db.execute("UPDATE photo.graph_expansions SET expansion_lease_until=clock_timestamp()-interval '1 second' WHERE id=%s", (x['expansion'],))
    else: db.execute("UPDATE photo.v30_delivery_runtime_leases SET expires_at=clock_timestamp()-interval '1 second' WHERE instance_id=%s", (x['instance'],))
    assert fail_source(db, x, **args) is False
    assert db.execute('SELECT state FROM photo.delivery_packages WHERE id=%s', (x['package'],)).fetchone()[0] == 'building'
    assert db.execute('SELECT state FROM photo.graph_expansions WHERE id=%s', (x['expansion'],)).fetchone()[0] == 'open'


def derivative_delivery(db, result_mutation=None):
    x = prepared_delivery(db, presentation=True)
    request = json.loads(x['request'])
    child = request['child_run_id']
    export = db.execute("SELECT photo.v30_uuid5(%s,'v30:delivery-export:1')",(child,)).fetchone()[0]
    version = request['binding']['asset_version_id']
    preset_sha = request['preset']['preset_sha']
    if result_mutation == 'request_binding':
        request={**request,'binding':{**request['binding'],'asset_version_id':str(uuid4())}}
    cfg = {'project_id': str(x['project']), 'v30_delivery_export': request, 'export_id': str(export)}
    db.execute("INSERT INTO photo.runs(failure_policy,id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES('fail_fast',%s,%s,'v2','running',%s,%s,%s,%s,false)", (child, x['tenant'], x['snapshot'], Jsonb(cfg), '9'*64, x['actor']))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(photo.v30_uuid5(%s,'workflow'),%s,%s,%s,%s,'delivery','v2.2',%s)",(child,x['tenant'],x['project'],child,x['snapshot'],'c'*64))
    task = db.execute("SELECT photo.v30_uuid5(photo.v30_uuid5(%s,'workflow'),'derivative.resize:0')", (child,)).fetchone()[0]
    result, worker = uuid4(), uuid4()
    params = x['source']['artifact_id']
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,canonical_result_id) VALUES(%s,%s,%s,'derivative.resize:0','derivative.resize','1','cpu','blocked','[]',%s,%s,%s,1,1,%s)", (task, x['tenant'], child, params, '7'*64, Jsonb({'renderer_version': 'v2.2'}), result))
    if result_mutation != 'missing_closure':
        seal_delivery_test_closure(db,x['tenant'],x['project'],task,[(x['source'],'Params')])
    if result_mutation != 'unsealed_run':
        db.execute("UPDATE photo.runs SET graph_sealed=true WHERE tenant_id=%s AND id=%s",(x['tenant'],child))
    db.execute("UPDATE photo.tasks SET state='succeeded' WHERE id=%s",(task,))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')", (x['tenant'], task, worker))
    # Production registers the export before the item-reference guard may bind it.
    db.execute("INSERT INTO photo.exports(id,tenant_id,asset_version_id,preset_hash,status,metadata_policy) VALUES(%s,%s,%s,%s,'queued',jsonb_build_object('metadata_policy','strip'))", (export, x['tenant'], version, preset_sha))
    db.execute("UPDATE photo.delivery_items SET export_id=%s,revision=revision+1 WHERE tenant_id=%s AND package_id=%s",(export,x['tenant'],x['package']))
    outputs = []
    for role in ['working_image', 'derivative_report', 'working_rgb']:
        aid = uuid4()
        digest = sha256(role.encode()).hexdigest()
        media = 'application/octet-stream' if role == 'working_rgb' else 'application/json'
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'photo',%s,'derivative-v1',%s,20,%s,'ready',clock_timestamp(),%s,%s,1,1)", (aid, x['tenant'], str(aid), digest, media, role, task))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,output_role,bucket,object_key,sha256,bytes,media_type,'ready',object_version,clock_timestamp() FROM photo.artifacts WHERE id=%s", (task, worker, 'a'*64, uuid4(), aid))
        outputs.append({'artifact_id': str(aid), 'sha256': digest})
    if result_mutation == 'sealed_parent':
        db.execute("UPDATE photo.runs SET graph_sealed=true WHERE id=%s",(x['run'],))
    elif result_mutation == 'duplicate_ref':
        outputs[1]=dict(outputs[0])
    elif result_mutation == 'wrong_sha':
        outputs[0]={**outputs[0],'sha256':'0'*64}
    elif result_mutation == 'expired_output':
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(outputs[0]['artifact_id'],))
    elif result_mutation == 'upload_key':
        db.execute("UPDATE photo.output_uploads SET object_key=object_key||'-other' WHERE artifact_id=%s",(outputs[0]['artifact_id'],))
    elif result_mutation == 'item_not_pending':
        db.execute("UPDATE photo.delivery_items SET state='failed',reason='SOURCE_OBJECT_MISSING',revision=revision+1 WHERE tenant_id=%s AND package_id=%s",(x['tenant'],x['package']))
    elif result_mutation == 'producer_fence':
        db.execute("UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s",(outputs[0]['artifact_id'],))
    db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)", (result, x['tenant'], task, Jsonb(outputs), '8'*64))
    stage = db.execute("SELECT photo.v30_delivery_stage_result(%s,%s,0,'derivative.resize')", (x['expansion'], x['token'])).fetchone()[0]
    assert stage is not None and stage['result_id'] == str(result)
    return {**x, 'source': outputs[2], 'request': '{}', 'derivative_task': task}


def test_delivery_derivative_corruption_uses_real_child_stage_authority(db):
    x = derivative_delivery(db)
    assert fail_source(db, x, version='derivative-v1', code='SOURCE_NPY_INVALID') is True
    assert db.execute('SELECT reason FROM photo.delivery_items WHERE package_id=%s', (x['package'],)).fetchone()[0] == 'SOURCE_NPY_INVALID'


def test_delivery_derivative_stale_producer_cannot_fail_package(db):
    x = derivative_delivery(db)
    db.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s', (x['derivative_task'],))
    assert fail_source(db, x, version='derivative-v1') is False
    assert db.execute('SELECT state FROM photo.delivery_packages WHERE id=%s', (x['package'],)).fetchone()[0] == 'building'


@pytest.mark.parametrize('field,value', [
    ('bucket', 'other-bucket'), ('object_key', 'replaced-object'),
    ('object_version', 'replaced-version'), ('bytes', 21),
    ('media_type', 'image/png'),
])
def test_delivery_derivative_upload_descriptor_drift_is_not_authority(db, field, value):
    x = derivative_delivery(db)
    # Change only accepted receipt metadata, retaining artifact SHA/canonical
    # result. A content hash cannot repair provenance.
    db.execute('UPDATE photo.output_uploads SET '+field+'=%s WHERE artifact_id=%s', (value, x['source']['artifact_id']))
    assert db.execute("SELECT photo.v30_delivery_stage_result(%s,%s,0,'derivative.resize')", (x['expansion'], x['token'])).fetchone()[0] is None
    assert fail_source(db, x, version='derivative-v1') is False
    assert db.execute('SELECT state FROM photo.delivery_packages WHERE id=%s', (x['package'],)).fetchone()[0] == 'building'


@pytest.mark.parametrize('status', ['completed', 'failed', 'cancelled'])
def test_delivery_source_failure_preserves_terminal_child_cancellation_state(db, status):
    x = prepared_delivery(db)
    child = db.execute("SELECT photo.v30_uuid5(%s,'child:export:0')", (x['run'],)).fetchone()[0]
    cfg = {'project_id': str(x['project']), 'v30_delivery_export': json.loads(x['request'])}
    db.execute("INSERT INTO photo.runs(failure_policy,id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES('fail_fast',%s,%s,'v2',%s,%s,%s,%s,%s,true)", (child, x['tenant'], status, x['snapshot'], Jsonb(cfg), '9'*64, x['actor']))
    before = db.execute('SELECT status,cancel_requested,revision,config_snapshot FROM photo.runs WHERE id=%s', (child,)).fetchone()
    assert fail_source(db, x) is True
    assert db.execute('SELECT status,cancel_requested,revision,config_snapshot FROM photo.runs WHERE id=%s', (child,)).fetchone() == before


def test_delivery_v2_child_rejects_partial_success_state(db):
    x = prepared_delivery(db)
    with pytest.raises(psycopg.errors.CheckViolation, match='runs_v2_fail_fast'):
        with db.transaction():
            db.execute("INSERT INTO photo.runs(failure_policy,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES('fail_fast',%s,'v2','completed_with_errors',%s,%s,%s,%s,true)", (x['tenant'],x['snapshot'],Jsonb({'project_id':str(x['project'])}),'9'*64,x['actor']))


@pytest.mark.parametrize('mutation',['duplicate_ref','wrong_sha','expired_output','upload_key','producer_fence','missing_closure','unsealed_run','request_binding','item_not_pending','sealed_parent'])
def test_delivery_typed_result_guard_rejects_unverified_physical_outputs(db,mutation):
    # Runs the actual installed task_results trigger, not a substituted predicate.
    expected = 'ARTIFACT_REFERENCE_NOT_READY' if mutation == 'expired_output' else 'DELIVERY_TYPED_RESULT_OUTPUTS_UNVERIFIED'
    with pytest.raises(psycopg.errors.CheckViolation,match=expected):
        with db.transaction():
            derivative_delivery(db,result_mutation=mutation)
