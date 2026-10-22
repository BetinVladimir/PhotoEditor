"""Published Delivery manifest bearer lifecycle against current SQL.

Seed privileged persisted state; this tests download authority, not assembly.
Authored only. No database or migration execution during coding.
"""
import json
from hashlib import sha256
from uuid import uuid4
import pytest
import psycopg
from psycopg.types.json import Jsonb
from test_database import tenant


def persisted_document_output(db, tenant_id, task_id, attempt=1, fence=1, artifact_id=None, role='document', body=None):
    """Seed the real canonical-result relational shape, not worker execution.

    Keep the installed root/upload guards enabled. Physical document bytes and
    algorithm execution are intentionally outside these authority fixtures.
    """
    if artifact_id is None:
        artifact_id = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'result-v1',%s,%s,'application/json','ready',clock_timestamp(),%s,%s,%s,%s) RETURNING id", (tenant_id,str(uuid4()),sha256(body.encode()).hexdigest() if body is not None else 'd'*64,len(body.encode()) if body is not None else 2,role,task_id,attempt,fence)).fetchone()[0]
    else:
        db.execute("UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=%s,produced_fence=%s WHERE tenant_id=%s AND id=%s", (task_id,attempt,fence,tenant_id,artifact_id))
    db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,%s,%s,%s,%s,%s,id,output_role,bucket,object_key,sha256,bytes,media_type,'ready',object_version,clock_timestamp() FROM photo.artifacts WHERE tenant_id=%s AND id=%s", (task_id,attempt,fence,uuid4(),'a'*64,uuid4(),tenant_id,artifact_id))
    db.execute("UPDATE photo.tasks SET execution_profile=execution_profile||jsonb_build_object('renderer_version','v2.2') WHERE tenant_id=%s AND id=%s", (tenant_id,task_id))
    sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant_id,artifact_id)).fetchone()[0]
    return {'artifact_id':str(artifact_id),'sha256':sha}



def seal_delivery_test_closure(db, tenant_id, project_id, task_id, entries=None):
    """Persist canonical transport documents, keeping input immutability enabled.

    Relational integration evidence only: objects are not physically uploaded.
    """
    from hashlib import sha256
    def document(value):
        raw=json.dumps(value,sort_keys=True,separators=(',',':'))
        digest=sha256(raw.encode()).hexdigest()
        aid=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,'photo',%s,'closure-v1',%s,%s,'application/json','ready',clock_timestamp()) RETURNING id",(tenant_id,str(uuid4()),digest,len(raw))).fetchone()[0]
        return {'artifact_id':str(aid),'sha256':digest}
    if entries is None:
        entries=[({'artifact_id':str(a),'sha256':h},c) for a,h,c in db.execute("SELECT artifact_id,sha256,contract FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s ORDER BY artifact_id",(tenant_id,task_id))]
    values=[{'ref':ref,'contract':contract} for ref,contract in sorted(entries,key=lambda e:e[0]['artifact_id'])]
    digest=sha256(json.dumps(values,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    chunk=document({'schema_version':1,'entries':values})
    manifest=document({'schema_version':1,'task_id':str(task_id),'entry_count':len(values),'entries_sha':digest,'chunks':[chunk]})
    existing={str(row[0]) for row in db.execute('SELECT artifact_id FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s',(tenant_id,task_id))}
    for ordinal,(ref,contract) in enumerate([*entries,(chunk,'InputClosureChunk'),(manifest,'InputClosureManifest')]):
        if ref['artifact_id'] not in existing:
            db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tenant_id,project_id,task_id,ref['artifact_id'],ref['sha256'],contract,ordinal))
    # Generic seal_v2_task_inputs freezes the baseline transport protocol pin;
    # the current overall JSON-schema digest belongs to other dedicated seals.
    profile={'backend':'cpu','renderer_version':'v2.2','contracts_sha':'4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4','v2_input_manifest':manifest,'v2_entry_count':len(values),'v2_entries_sha':digest}
    db.execute('UPDATE photo.tasks SET inputs=%s,execution_profile=execution_profile||%s WHERE tenant_id=%s AND id=%s',(Jsonb([manifest]),Jsonb(profile),tenant_id,task_id))
    assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task_id,)).fetchone()[0] is True


def published_manifest(db, presets=None, required_signing=False, stage_building=False):
    tid, actor = tenant(db, str(uuid4()) + '@delivery-download.test')
    session = db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,%s,'native',clock_timestamp()+interval '1 hour') RETURNING id", (actor, uuid4().bytes + uuid4().bytes, uuid4())).fetchone()[0]
    project = db.execute("INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES(%s,'Download','workspace',true) RETURNING id", (tid,)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,1) RETURNING id", (tid, actor, 'a'*64)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(failure_policy,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES('fail_fast',%s,'v2',%s,%s,jsonb_build_object('project_id',%s::text),%s,%s,true) RETURNING id", (tid, 'running' if stage_building else 'completed', snapshot, str(project), 'b'*64, actor)).fetchone()[0]
    workflow = db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'delivery','v2.2',%s) RETURNING id", (tid, project, run, snapshot, 'c'*64)).fetchone()[0]
    def artifact(role=None):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(%s,'photo',%s,'pinned-v1',%s,2,'application/json','ready',clock_timestamp(),%s) RETURNING id", (tid, str(uuid4()), 'd'*64, role)).fetchone()[0]
    policy, params, manifest = artifact(), artifact(), artifact('document')
    prepare = db.execute("SELECT photo.v30_uuid5(%s,'v2.delivery.prepare:0')", (workflow,)).fetchone()[0]
    prepare_result = uuid4()
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,canonical_result_id) VALUES(%s,%s,%s,'prepare','v2.delivery.prepare','1','cpu','succeeded','[]',%s,%s,'{}',1,1,%s)", (prepare, tid, run, params, 'e'*64, prepare_result))
    signing_policy = uuid4() if required_signing else None
    signing_sha = '6'*64
    frozen_policy = {'provenance': 'off'} if not required_signing else {'provenance': 'required', 'signing_policy_id': str(signing_policy), 'signing_policy_sha': signing_sha}
    signing_artifact = None
    if required_signing:
        signing_artifact = db.execute("SELECT photo.v30_uuid5(%s,%s)", (tid, 'signing:policy:'+str(signing_policy)+':'+signing_sha)).fetchone()[0]
        policy_key = 'tenants/'+str(tid)+'/runs/'+str(run)+'/documents/'+str(signing_artifact)
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(%s,%s,'photo',%s,'policy-v1',%s,2,'application/json','ready',clock_timestamp(),'v31_signing_policy')", (signing_artifact, tid, policy_key, signing_sha))
        db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,'v31_signing_policy',%s,%s,%s,2,'photo','ready','policy-v1',clock_timestamp())", (tid, run, signing_artifact, signing_artifact, uuid4(), signing_sha))
    db.execute('SET LOCAL ROLE photo_fn_owner')
    db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,v30_delivery_proof) VALUES(%s,%s,%s,'task_params',%s,%s,%s,2,'photo','ready','pinned-v1',clock_timestamp(),%s)", (tid, run, prepare, params, uuid4(), 'd'*64, Jsonb({'frozen_input_jcs': json.dumps({'policy': frozen_policy, 'presets': presets or [], 'snapshot_digest': 'a'*64})})))
    db.execute('RESET ROLE')
    prepare_root = persisted_document_output(db,tid,prepare)
    db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)",(prepare_result,tid,prepare,Jsonb([prepare_root]),'f'*64))
    package = db.execute("INSERT INTO photo.delivery_packages(tenant_id,project_id,workflow_id,snapshot_id,name,policy_artifact_id,manifest_artifact_id,published_at,state) VALUES(%s,%s,%s,%s,'Published',%s,%s,CASE WHEN %s THEN NULL ELSE clock_timestamp() END,%s) RETURNING id", (tid, project, workflow, snapshot, policy, manifest, stage_building, 'building' if stage_building else 'ready')).fetchone()[0]
    if required_signing:
        closure = json.dumps({'tenant_id': str(tid), 'actor_id': str(actor), 'parent_id': str(run), 'policy_id': str(signing_policy), 'policy_sha': signing_sha, 'entries': []}, sort_keys=True, separators=(',', ':'))
        from hashlib import sha256
        db.execute("INSERT INTO photo.delivery_signing_closures(tenant_id,project_id,package_id,parent_run_id,actor_id,policy_id,policy_sha,closure_sha,closure_jcs) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s)", (tid, project, package, run, actor, signing_policy, signing_sha, sha256(closure.encode()).hexdigest(), closure))
    return dict(signing_artifact=signing_artifact, signing_policy=signing_policy, tenant=tid, actor=actor, session=session, project=project, package=package, manifest=manifest, snapshot=snapshot, run=run)


def issue(db, x, preset=None):
    token = uuid4().bytes + uuid4().bytes
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(x['tenant']),))
    db.execute('SET LOCAL ROLE photo_api')
    created = db.execute("SELECT photo.create_v30_delivery_download_intent(%s,%s,%s,%s,%s,%s,%s,'photo',%s,%s,clock_timestamp()+interval '45 seconds')", (x['tenant'], x['actor'], x['session'], x['package'], 'manifest' if preset is None else 'slot', None if preset is None else 0, preset, uuid4(), token)).fetchone()[0]
    db.execute('RESET ROLE')
    assert created is True  # Every negative scenario starts with a usable target.
    return token


def redeem(db, token):
    db.execute('SET LOCAL ROLE photo_portal')
    result = db.execute("SELECT * FROM photo.redeem_download_intent(%s,'photo')", (token,)).fetchone()
    db.execute('RESET ROLE')
    return result


def test_delivery_manifest_success_is_pinned_and_one_use(db):
    x = published_manifest(db)
    token = issue(db, x)
    row = redeem(db, token)
    assert row[0] == 'ok' and row[2] == 'pinned-v1' and row[3] == 'd'*64
    assert row[4:6] == (2, 'application/json')
    assert redeem(db, token)[0] == 'invalid'


@pytest.mark.parametrize('change', ['expiry', 'project_archive', 'download_disabled', 'session_revoked', 'unpublished', 'object_replaced'])
def test_delivery_manifest_late_revocation_never_consumes_bearer(db, change):
    x = published_manifest(db)
    token = issue(db, x)
    if change == 'expiry':
        db.execute("UPDATE photo.download_intents SET expires_at=clock_timestamp()-interval '1 second' WHERE token_hash=%s", (token,))
    elif change == 'project_archive':
        db.execute('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s', (x['project'],))
    elif change == 'download_disabled':
        db.execute('UPDATE photo.projects SET download_allowed=false WHERE id=%s', (x['project'],))
    elif change == 'session_revoked':
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (x['session'],))
    elif change == 'unpublished':
        with pytest.raises(psycopg.errors.CheckViolation,match='DELIVERY_PUBLICATION_IMMUTABLE'):
            with db.transaction():
                db.execute('UPDATE photo.delivery_packages SET published_at=NULL WHERE id=%s', (x['package'],))
        db.execute('UPDATE photo.projects SET download_allowed=false WHERE id=%s',(x['project'],))
    else:
        # Content coordinates are immutable. Replace the package pin with a
        # distinct ready artifact, as a new publication would, leaving the old
        # bearer bound to its original exact object.
        replacement = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(%s,'photo',%s,'replaced-v2',%s,2,'application/json','ready',clock_timestamp(),'document') RETURNING id", (x['tenant'],str(uuid4()),'d'*64)).fetchone()[0]
        with pytest.raises(psycopg.errors.CheckViolation,match='DELIVERY_MANIFEST_IMMUTABLE'):
            with db.transaction():
                db.execute('UPDATE photo.delivery_packages SET manifest_artifact_id=%s WHERE id=%s', (replacement,x['package']))
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(x['manifest'],))
    assert redeem(db, token)[0] in ('invalid', 'revoked')
    assert db.execute('SELECT consumed_at FROM photo.download_intents WHERE token_hash=%s', (token,)).fetchone()[0] is None


def test_delivery_manifest_cannot_issue_for_foreign_tenant_or_actor(db):
    x = published_manifest(db)
    issue(db, x)
    for tenant_id, actor_id in [(uuid4(), x['actor']), (x['tenant'], uuid4())]:
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant_id),))
        db.execute('SET LOCAL ROLE photo_api')
        created = db.execute("SELECT photo.create_v30_delivery_download_intent(%s,%s,%s,%s,'manifest',NULL,NULL,'photo',%s,%s,clock_timestamp()+interval '45 seconds')", (tenant_id, actor_id, x['session'], x['package'], uuid4(), uuid4().bytes + uuid4().bytes)).fetchone()[0]
        db.execute('RESET ROLE')
        assert created is False


def published_slot(db, required_signing=False):
    from test_delivery_physical_source_failure import prepared_delivery
    x=prepared_delivery(db,required_signing=required_signing)
    request=json.loads(x['request'])
    tid,project=x['tenant'],x['project']
    preset=request['preset']['preset_id'];preset_sha=request['preset']['preset_sha']
    version=request['binding']['asset_version_id'];master=request['binding']['source']['artifact_id']
    child=request['child_run_id']
    export=db.execute("SELECT photo.v30_uuid5(%s,'v30:delivery-export:1')",(child,)).fetchone()[0]
    manifest=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(%s,'photo',%s,'pinned-v1',%s,2,'application/json','ready',clock_timestamp(),'document') RETURNING id",(tid,str(uuid4()),'d'*64)).fetchone()[0]
    x['manifest']=manifest
    cfg={'project_id':str(project),'export_id':str(export),'v30_delivery_export':request}
    db.execute("INSERT INTO photo.runs(failure_policy,id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES('fail_fast',%s,%s,'v2','running',%s,%s,%s,%s,false)", (child, tid, x['snapshot'], Jsonb(cfg), '2'*64, x['actor']))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(photo.v30_uuid5(%s,'workflow'),%s,%s,%s,%s,'delivery','v2.2',%s)", (child, tid, project, child, x['snapshot'], 'c'*64))
    task = db.execute("SELECT photo.v30_uuid5(photo.v30_uuid5(%s,'workflow'),'export.encode:0')", (child,)).fetchone()[0]
    result = uuid4()
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,canonical_result_id) VALUES(%s,%s,%s,'export.encode:0','export.encode','1','cpu','blocked','[]',%s,%s,jsonb_build_object('backend','cpu'),1,1,%s)", (task, tid, child, master, '3'*64, result))
    source_ref=request['binding']['source']
    seal_delivery_test_closure(db,tid,project,task,[(source_ref,'Params')])
    db.execute("UPDATE photo.runs SET graph_sealed=true WHERE tenant_id=%s AND id=%s",(tid,child))
    db.execute("UPDATE photo.tasks SET state='succeeded' WHERE id=%s",(task,))
    encoded = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'encoded-v1',%s,3,'image/jpeg','ready',clock_timestamp(),'encoded_image',%s,1,1) RETURNING id", (tid, str(uuid4()), '4'*64, task)).fetchone()[0]
    db.execute("INSERT INTO photo.exports(id,tenant_id,asset_version_id,preset_hash,status,task_id,metadata_policy) VALUES(%s,%s,%s,%s,'queued',%s,jsonb_build_object('metadata_policy','strip'))", (export, tid, version, preset_sha, task))
    db.execute("UPDATE photo.delivery_items SET export_id=%s,revision=revision+1 WHERE tenant_id=%s AND package_id=%s",(export,tid,x['package']))
    outputs = [persisted_document_output(db,tid,task,artifact_id=encoded), persisted_document_output(db,tid,task,role='encoded_manifest')]
    db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)", (result, tid, task, Jsonb(outputs), '5'*64))
    # Persisted authority only: physical encode verification is separate.
    for consumer in ('v2-adopt', 'v30-encode-verified'):
        db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,%s,photo.v2_adoption_event_id(%s,%s),clock_timestamp())", (tid, consumer, task, result))
    db.execute("UPDATE photo.exports SET artifact_id=%s,status='ready',task_id=%s WHERE tenant_id=%s AND id=%s", (encoded, task, tid, export))
    db.execute("UPDATE photo.delivery_items SET state='ready',revision=revision+1 WHERE tenant_id=%s AND package_id=%s", (tid,x['package']))
    # Actual assembly activation reseals the parent before publication (01273).
    db.execute("UPDATE photo.runs SET graph_sealed=true,status='completed' WHERE tenant_id=%s AND id IN(%s,%s)", (tid,child,x['run']))
    db.execute("UPDATE photo.delivery_packages SET state='ready',manifest_artifact_id=%s,published_at=clock_timestamp(),revision=revision+1 WHERE tenant_id=%s AND id=%s", (manifest,tid,x['package']))
    return {**x, 'preset': preset, 'task': task, 'result': result, 'encoded': encoded, 'export': export}


def test_delivery_slot_requires_actual_adopted_export_lineage(db):
    x = published_slot(db)
    row = redeem(db, issue(db, x, x['preset']))
    assert row[0] == 'ok' and row[2:6] == ('encoded-v1', '4'*64, 3, 'image/jpeg')


@pytest.mark.parametrize('change', ['adoption_missing', 'canonical_result_missing', 'producer_fence_changed'])
def test_delivery_slot_rechecks_lineage_after_intent_creation(db, change):
    x = published_slot(db)
    token = issue(db, x, x['preset'])
    if change == 'adoption_missing':
        db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'", (x['tenant'],))
    elif change == 'canonical_result_missing':
        db.execute("UPDATE photo.tasks SET state='blocked',canonical_result_id=NULL WHERE id=%s", (x['task'],))
    else:
        db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s', (x['encoded'],))
    assert redeem(db, token)[0] == 'revoked'
    assert db.execute('SELECT consumed_at FROM photo.download_intents WHERE token_hash=%s', (token,)).fetchone()[0] is None


def post_sign_current(db, token, row):
    db.execute('SET LOCAL ROLE photo_portal')
    value = db.execute("SELECT photo.v30_delivery_download_intent_current(%s,'photo',%s,%s,%s,%s,%s)", (token, row[1], row[2], row[3], row[4], row[5])).fetchone()[0]
    db.execute('RESET ROLE')
    return value


@pytest.mark.parametrize('kind', ['manifest', 'slot'])
def test_delivery_postsign_rechecks_exact_object_and_current_access(db, kind):
    x = published_manifest(db) if kind == 'manifest' else published_slot(db)
    token = issue(db, x, x.get('preset'))
    assert post_sign_current(db, token, ('ok', 'unknown', 'v1', '0'*64, 1, 'application/json')) is False
    row = redeem(db, token)
    assert row[0] == 'ok'
    assert post_sign_current(db, token, row) is True
    for field, replacement in [(1, 'different-object'), (2, 'different-version'),
                               (3, '0'*64), (4, row[4]+1), (5, 'application/octet-stream')]:
        changed = list(row)
        changed[field] = replacement
        assert post_sign_current(db, token, changed) is False
    # A signer may take time; authority is repeated after it has returned.
    db.execute('UPDATE photo.projects SET download_allowed=false WHERE id=%s', (x['project'],))
    assert post_sign_current(db, token, row) is False
    # The original bearer remains one-use even when delivery is now refused.
    assert redeem(db, token)[0] == 'invalid'


@pytest.mark.parametrize('kind', ['manifest', 'slot'])
def test_delivery_postsign_restores_callers_tenant_context(db, kind):
    x = published_manifest(db) if kind == 'manifest' else published_slot(db)
    token = issue(db, x, x.get('preset'))
    row = redeem(db, token)
    assert row[0] == 'ok'
    previous = str(uuid4())
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (previous,))
    assert post_sign_current(db, token, row) is True
    assert db.execute("SELECT current_setting('app.tenant_id',true)").fetchone()[0] == previous


@pytest.mark.parametrize('change', ['closure_removed', 'policy_expired'])
def test_required_signing_manifest_rechecks_retained_policy_and_closure(db, change):
    x = published_manifest(db, required_signing=True)
    token = issue(db, x)
    row = redeem(db, token)
    assert row[0] == 'ok'
    assert post_sign_current(db, token, row) is True
    if change == 'closure_removed':
        db.execute('DELETE FROM photo.delivery_signing_closures WHERE tenant_id=%s AND package_id=%s', (x['tenant'], x['package']))
    else:
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s", (x['signing_artifact'],))
    assert post_sign_current(db, token, row) is False
    # A new intent cannot bypass revoked signing authority either.
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(x['tenant']),))
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute("SELECT photo.create_v30_delivery_download_intent(%s,%s,%s,%s,'manifest',NULL,NULL,'photo',%s,%s,clock_timestamp()+interval '45 seconds')", (x['tenant'], x['actor'], x['session'], x['package'], uuid4(), uuid4().bytes + uuid4().bytes)).fetchone()[0] is False
    db.execute('RESET ROLE')


def published_signed_slot(db):
    from hashlib import sha256
    x = published_slot(db, required_signing=True)
    tid, project = x['tenant'], x['project']
    key = str(uuid4())
    signer_workflow = db.execute('SELECT photo.v30_uuid5(%s,%s)', (tid, 'provenance:'+str(x['actor'])+':'+key)).fetchone()[0]
    signer_run, signer_task, signer_result, signed = uuid4(), uuid4(), uuid4(), uuid4()
    db.execute("INSERT INTO photo.runs(failure_policy,id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES('fail_fast',%s,%s,'v2','completed',%s,jsonb_build_object('project_id',%s::text),%s,%s,true)", (signer_run, tid, x['snapshot'], str(project), '7'*64, x['actor']))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'provenance','v2.2',%s)", (signer_workflow, tid, project, signer_run, x['snapshot'], '6'*64))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,canonical_result_id) VALUES(%s,%s,%s,'sign','provenance.sign','1','cpu','succeeded','[]',%s,%s,'{}',1,1,%s)", (signer_task, tid, signer_run, x['manifest'], '8'*64, signer_result))
    object_key = 'tenants/'+str(tid)+'/tasks/'+str(signer_task)+'/attempts/1/fences/1/'+str(signed)
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'photo',%s,'signed-v1',%s,4,'image/jpeg','ready',clock_timestamp(),'signed_export',%s,1,1)", (signed, tid, object_key, '9'*64, signer_task))
    db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)", (signer_result, tid, signer_task, Jsonb([persisted_document_output(db,tid,signer_task)]), 'a'*64))
    # Provenance's binary C2PA manifest is distinct from Delivery's JSON index.
    provenance_manifest = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'c2pa-v1',%s,2,'application/c2pa','ready',clock_timestamp(),'manifest',%s,1,1) RETURNING id", (tid,str(uuid4()),'c'*64,signer_task)).fetchone()[0]
    db.execute("INSERT INTO photo.provenance_manifests(tenant_id,project_id,export_id,workflow_id,manifest_artifact_id,signed_artifact_id,signer_key_id,signing_policy_sha,adopted_task_id,adopted_result_id,validation_sha) VALUES(%s,%s,%s,%s,%s,%s,'test-key',%s,%s,%s,%s)", (tid, project, x['export'], signer_workflow, provenance_manifest, signed, '6'*64, signer_task, signer_result, 'b'*64))
    body = {'tenant_id': str(tid), 'actor_id': str(x['actor']), 'parent_id': str(x['run']), 'policy_id': str(x['signing_policy']), 'policy_sha': '6'*64, 'entries': [{'ordinal': 0, 'preset_id': str(x['preset']), 'export_id': str(x['export']), 'export': {'artifact_id': str(x['encoded']), 'sha256': '4'*64}, 'task_id': str(signer_task), 'result_id': str(signer_result), 'validation_sha': 'b'*64, 'signing_key': key, 'signed': {'artifact_id': str(signed), 'sha256': '9'*64}}]}
    closure = json.dumps(body, sort_keys=True, separators=(',', ':'))
    # Seed the final retained closure before issuance; no runtime mutation is simulated.
    db.execute('DELETE FROM photo.delivery_signing_closures WHERE tenant_id=%s AND package_id=%s', (tid, x['package']))
    db.execute("INSERT INTO photo.delivery_signing_closures(tenant_id,project_id,package_id,parent_run_id,actor_id,policy_id,policy_sha,closure_sha,closure_jcs) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s)", (tid, project, x['package'], x['run'], x['actor'], x['signing_policy'], '6'*64, sha256(closure.encode()).hexdigest(), closure))
    return {**x, 'signed': signed, 'signer_task': signer_task}


def test_signed_delivery_slot_selects_verified_signed_bytes(db):
    x = published_signed_slot(db)
    token = issue(db, x, x['preset'])
    row = redeem(db, token)
    assert row[0] == 'ok' and row[2:6] == ('signed-v1', '9'*64, 4, 'image/jpeg')
    assert post_sign_current(db, token, row) is True


@pytest.mark.parametrize('change', ['signer_cancelled', 'signed_fence_changed', 'signer_task_fence_changed', 'policy_expired'])
def test_signed_delivery_slot_rechecks_signer_and_policy_after_signing(db, change):
    x = published_signed_slot(db)
    token = issue(db, x, x['preset'])
    row = redeem(db, token)
    assert row[0] == 'ok' and post_sign_current(db, token, row) is True
    if change == 'signer_cancelled':
        db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s', (x['signer_task'],))
    elif change == 'signer_task_fence_changed':
        db.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s', (x['signer_task'],))
    elif change == 'signed_fence_changed':
        db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s', (x['signed'],))
    else:
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s", (x['signing_artifact'],))
    assert post_sign_current(db, token, row) is False


def test_delivery_signing_closure_row_lock_does_not_allow_content_update(db):
    x=published_manifest(db,required_signing=True)
    guard=db.execute("SELECT t.tgtype,pg_get_userbyid(p.proowner),p.prosecdef,t.tgenabled FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid WHERE t.tgrelid='photo.delivery_signing_closures'::regclass AND t.tgname='delivery_signing_closure_immutable'").fetchone()
    assert guard==(19,'photo_fn_owner',False,'O')
    db.execute('SET LOCAL ROLE photo_fn_owner')
    assert db.execute('SELECT package_id FROM photo.delivery_signing_closures WHERE tenant_id=%s AND package_id=%s FOR SHARE',(x['tenant'],x['package'])).fetchone()[0]==x['package']
    with pytest.raises(psycopg.errors.CheckViolation,match='DELIVERY_SIGNING_CLOSURE_IMMUTABLE'):
        with db.transaction():
            db.execute('UPDATE photo.delivery_signing_closures SET closure_sha=closure_sha WHERE tenant_id=%s AND package_id=%s',(x['tenant'],x['package']))
    db.execute('RESET ROLE')
    for role in ['photo_api','photo_portal','photo_worker','photo_scheduler','photo_gateway','photo_maintenance']:
        assert db.execute("SELECT has_column_privilege(%s,'photo.delivery_signing_closures','closure_sha','UPDATE')",(role,)).fetchone()[0] is False
