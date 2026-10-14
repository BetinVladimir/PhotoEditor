"""Target graph/registration metadata gates. Not physical source or ML evidence."""
from copy import deepcopy
from hashlib import sha256
from uuid import UUID,uuid4,uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from test_v27_target_checkpoint import seed_checkpoint,save,ROLES


def seed_target_registration(db):
    packet,case,_,manifest=seed_checkpoint(db)
    saved=save(db,packet,canonical_bytes(manifest).decode());assert saved
    entry=manifest['entries'][1];namespace=UUID(entry['namespace'])
    run=uuid5(namespace,'semantic.mask.person:reference');workflow=uuid5(run,'workflow');task=uuid5(workflow,'mask.person:0')
    params=packet['registration']['params']
    profile=dict(backend='cuda',renderer_version='v2.2',model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64)
    stage=dict(schema_version=1,parent_run_id=packet['parent_run_id'],request_sha=packet['request_sha'],auth_session_id=case['Session'],target_ordinal=1,target_namespace=str(namespace),target_stage_id=entry['stages'][0]['id'],manifest_sha=saved['sha'],source=entry['binding'],intent=manifest['intent'],snapshot=manifest['snapshot'],params_document=str(uuid5(namespace,'semantic.mask.person.params:reference')),params=params,task_id=str(task),input_digest='d'*64,policy_sha='a'*64,recovery={})
    config=dict(project_id=case['Project'],v27_target_person_prepare=stage)
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy) VALUES(%s,%s,'v2',%s,%s,%s,%s,'draft',false,'fail_fast')",(run,case['Tenant'],case['Snapshot'],Jsonb(config),sha256(canonical_bytes(config)).hexdigest(),case['Actor']))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(workflow,case['Tenant'],case['Project'],run,case['Snapshot'],'a'*64))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'mask.person:0','mask.person','1','gpu','[]',%s,%s,%s)",(task,case['Tenant'],run,params['artifact_id'],'d'*64,Jsonb(profile)))
    pixel=dict(artifact_id=str(uuid4()),sha256='8'*64)
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'test',%s,%s,128,'application/x-npy','v1',clock_timestamp())",(pixel['artifact_id'],case['Tenant'],str(uuid4()),pixel['sha256']))
    direct=[params,entry['binding']['source'],pixel]
    rows=sorted([dict(ref=ref,contract=contract) for ref,contract in zip(direct,['Params','WorkingImage','NpyArray'])],key=lambda row:row['ref']['artifact_id'])
    chunk=dict(artifact_id=str(uuid4()),sha256='e'*64);transport=dict(artifact_id=str(uuid4()),sha256='f'*64)
    return packet,case,config,run,task,params,profile,rows,direct,[chunk],transport


def test_target_registration_scope_missing_and_partial_refusal(db):
    packet,case,config,run,task,params,profile,rows,direct,chunks,manifest=seed_target_registration(db)
    query='SELECT photo.semantic_target_person_registration_state(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
    def state(entries=rows,bootstrap=direct,count=3,entries_sha=None,scope=case['Tenant'],child=run,task_id=task,closure=manifest,parts=chunks):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(scope,))
            result=db.execute(query,(scope,child,task_id,params['artifact_id'],params['sha256'],'d'*64,Jsonb(profile),Jsonb(entries),Jsonb(closure),Jsonb(parts),Jsonb(bootstrap),count,entries_sha if entries_sha is not None else sha256(canonical_bytes(entries)).hexdigest())).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert state()=='missing'
    for args in [dict(scope=str(uuid4())),dict(child=uuid4()),dict(task_id=uuid4()),dict(count=None),dict(count=4),dict(entries_sha='a'*64),dict(parts=[]),dict(closure=[]),dict(bootstrap=direct[::-1]),dict(entries=rows[:-1])]:
        assert state(**args)=='conflict',args
    for field,value in [('parent_run_id',str(uuid4())),('request_sha','f'*64),('auth_session_id',str(uuid4())),('target_ordinal',0),('target_namespace',str(uuid4())),('target_stage_id',str(uuid4())),('manifest_sha','a'*64),('source',{}),('params_document',str(uuid4())),('task_id',str(uuid4()))]:
        # The immutable config gate rejects drift before registration can see it.
        with pytest.raises(errors.ObjectNotInPrerequisiteState, match="V2_RUN_CONFIG_IMMUTABLE"):
            with db.transaction(force_rollback=True):
                changed=deepcopy(config);changed['v27_target_person_prepare'][field]=value
                db.execute('UPDATE photo.runs SET config_snapshot=%s WHERE tenant_id=%s AND id=%s',(Jsonb(changed),case['Tenant'],run))
        assert state()=='missing'
    for mutation,args in [
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(case['Project'],)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(packet['run_id'],)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(mutation,args);assert state()=='conflict'
    # Partial registration cannot be treated as missing and silently repaired.
    with db.transaction(force_rollback=True):
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,ordinal,artifact_id,sha256,contract) VALUES(%s,%s,%s,0,%s,%s,'WorkingImage')",(case['Tenant'],case['Project'],task,direct[1]['artifact_id'],direct[1]['sha256']))
        assert state()=='conflict'
    assert state()=='missing'


def test_target_registration_roles_and_reference_isolation(db):
    packet,case,config,run,task,params,profile,rows,direct,chunks,manifest=seed_target_registration(db)
    signature='photo.semantic_target_person_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)'
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]==(role=='photo_scheduler')
        assert not db.execute("SELECT has_function_privilege(%s,'photo.lock_v27_target_person_graph_scope(uuid,uuid,uuid)','EXECUTE')",(role,)).fetchone()[0]
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                db.execute('SET LOCAL ROLE '+role)
                db.execute('SELECT photo.lock_v27_target_person_graph_scope(%s,%s,%s)',(case['Tenant'],run,task))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        # Target config never inherits the original reference registration gate.
        result=db.execute('SELECT photo.semantic_person_preparation_registration_state(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(case['Tenant'],run,task,params['artifact_id'],params['sha256'],'d'*64,Jsonb(profile),Jsonb(rows),Jsonb(manifest),Jsonb(chunks),Jsonb(direct),3,sha256(canonical_bytes(rows)).hexdigest())).fetchone()[0]
        assert result=='conflict'
        db.execute('RESET ROLE')


def test_target_admission_roles_and_malformed_envelope(db):
    packet,case,config,run,task,params,profile,rows,direct,chunks,manifest=seed_target_registration(db)
    signature='photo.lock_semantic_target_person_admission_evidence(uuid,uuid,jsonb)'
    private='photo.semantic_target_person_registration_binding_state(uuid,uuid,jsonb)'
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]==(role=='photo_scheduler')
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,private)).fetchone()[0]
        if role!='photo_scheduler':
            with pytest.raises(errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute('SET LOCAL ROLE '+role)
                    db.execute('SELECT photo.lock_semantic_target_person_admission_evidence(%s,%s,%s)',(case['Tenant'],run,Jsonb({})))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        for body in [None,[],{},dict(schema_version=2),dict(schema_version=1,original={},target={}),dict(schema_version=1,original={},target={},extra=True)]:
            assert db.execute('SELECT photo.lock_semantic_target_person_admission_evidence(%s,%s,%s)',(case['Tenant'],run,Jsonb(body))).fetchone()==(False,)
        db.execute('RESET ROLE')


def test_target_seal_roles_and_empty_packets(db):
    packet,case,config,run,task,params,profile,rows,direct,chunks,manifest=seed_target_registration(db)
    signature='photo.seal_semantic_target_person_preparation(uuid,uuid,jsonb)'
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]==(role=='photo_scheduler')
        if role!='photo_scheduler':
            with pytest.raises(errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute('SET LOCAL ROLE '+role)
                    db.execute('SELECT photo.seal_semantic_target_person_preparation(%s,%s,%s)',(case['Tenant'],run,Jsonb({})))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        for body in [None,[],{},dict(schema_version=2),dict(schema_version=1,original={},target={}),dict(schema_version=1,original={},target={},extra=True)]:
            assert db.execute('SELECT photo.seal_semantic_target_person_preparation(%s,%s,%s)',(case['Tenant'],run,Jsonb(body))).fetchone()==(None,)
        db.execute('RESET ROLE')
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()==('draft',False)
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('blocked',)
    assert db.execute('SELECT count(*) FROM photo.run_events WHERE run_id=%s',(run,)).fetchone()==(0,)
