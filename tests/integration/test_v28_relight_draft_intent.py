from copy import deepcopy
from uuid import uuid4,uuid5
from hashlib import sha256
import json
from pathlib import Path
import pytest
import psycopg
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from test_v27_semantic_final_producer_gate import fixture
from semantic_qualification_fixture import frozen_qualification


@pytest.mark.migration_head(995)
def test_relight_intent_upgrade_preserves_existing_runs(database):
    with psycopg.connect(**database) as setup:
        seed=fixture(setup,snapshot_authority=True)
        count=setup.execute('SELECT count(*) FROM photo.runs').fetchone()[0]
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/00996_v28_relight_draft_intent.sql'
    with psycopg.connect(**database) as upgraded:
        upgraded.execute(migration.read_text().split('-- +goose Down')[0])
        assert upgraded.execute('SELECT count(*) FROM photo.runs WHERE v28_relight_intent IS NULL').fetchone()[0]==count
    with psycopg.connect(**database) as checked:
        check_relight_intent(checked,seed)


def test_relight_intent_frozen_replay_and_revocation(db):
    check_relight_intent(db,fixture(db,snapshot_authority=True))


def test_relight_intent_original_depth_discovery_and_revocation(db):
    check_relight_intent(db,fixture(db,snapshot_authority=True),verify_depth=True)


@pytest.mark.migration_head(996)
def test_relight_intent_depth_origin_upgrade(db):
    seed=fixture(db,snapshot_authority=True)
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/00997_v28_relight_intent_depth_origin.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    check_relight_intent(db,seed,verify_depth=True)


def check_relight_intent(db,seed,verify_depth=False,objects=None,verify_person=False,verify_manual=False,external_vectors=False):
    tenant,*_,scope=seed
    pin=frozen_qualification(db,tenant,scope['project'],scope['actor'],operation='image.relight',backend='cpu')
    pin['policy_sha']=db.execute('SELECT photo.v28_relight_policy_sha(%s)',(pin['policy_id'],)).fetchone()[0]
    run,workflow=uuid4(),uuid4()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'v2','draft',%s,%s,'b'||repeat('b',63),%s,'fail_fast',false)",(run,tenant,scope['snapshot'],Jsonb({'project_id':str(scope['project'])}),scope['actor']))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',repeat('b',64))",(workflow,tenant,scope['project'],run,scope['snapshot']))
    ref=lambda:dict(artifact_id=str(uuid4()),sha256='a'*64)
    params=dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),strength=.7,depth=ref(),subject=ref(),protected=ref(),calibration=pin['calibration'],lights=[dict(x=.5,y=.5,radius=.2,ev=1)])
    if scope.get('source_body') is not None:
        params['base']['canvas_hash']=canonical_hash(json.loads(scope['source_body'])['canvas'])
    retained={}
    if verify_depth:
        from test_v28_relight_depth import seed_depth_inputs
        seed_depth_inputs(db,tenant,scope,retained,objects=objects)
        params['depth']=scope['depth_ref']
    if verify_person:
        from test_v28_relight_person import seed_person_inputs
        seed_person_inputs(db,tenant,scope,retained,objects=objects)
        params['subject']=scope['subject_ref']
        guard=json.loads(retained['body']);guard.update(selector='manual',protected=True,model_lock_id=None)
        guard['artifact']=ref()
        retained['protected_body']=canonical_bytes(guard)
        params['protected']=dict(artifact_id=str(uuid4()),sha256=sha256(retained['protected_body']).hexdigest())
    manual_entries=[]
    if verify_manual:
        from test_v28_relight_manual import seed_manual_inputs
        def publish(aid,raw,role):
            from io import BytesIO
            client,bucket=objects
            media='application/octet-stream' if role in ('mask_target','mask_protected') else 'image/png' if role.endswith('_png') else 'application/json'
            version=client.put_object(bucket,str(aid),BytesIO(raw),len(raw),content_type=media,metadata={'sha256':sha256(raw).hexdigest()}).version_id
            return bucket,str(aid),version
        manual_entries=seed_manual_inputs(db,tenant,scope,retained,physical=objects is not None,publish=publish if objects is not None else None,external=external_vectors)
        retained['original']=retained['raw_objects'][retained['params']];retained['key']=str(retained['params'])
        params['subject']=scope['subject_ref'];params['protected']=scope['protected_ref']
    profile=dict(backend='cpu',renderer_version='v2.2',relight_runtime_sha=pin['runtime_lock_sha'],relight_qualification=pin,resource_estimate=dict(output_bytes=1000))
    inputs=[dict(ref=value,contract=contract) for value,contract in ((params['base']['source'],'WorkingImage'),(params['depth'],'RelativeDepth'),(params['subject'],'MaskRef'),(params['protected'],'MaskRef'),(params['calibration'],'Calibration'),(pin['report'],'QualificationReport'))]
    inputs += [dict(ref=ref(),contract='NpyArray') for _ in range(4)]
    if verify_depth:
        depth=json.loads(retained['body'])
        inputs[-4]['ref']=depth['artifact'];inputs[-3]['ref']=depth['invalid_mask']
    if verify_person:
        inputs[-4]['ref']=json.loads(retained['body'])['artifact']
        inputs[-3]['ref']=json.loads(retained['protected_body'])['artifact']
    if verify_manual:
        existing={value['ref']['artifact_id'] for value in inputs}
        inputs += [dict(ref=dict(artifact_id=str(aid),sha256=sha),contract=contract) for aid,sha,contract in manual_entries if str(aid) not in existing]
    inputs.sort(key=lambda value:value['ref']['artifact_id'])
    request=dict(actor_id=str(scope['actor']),auth_session_id=str(scope['session']),node_key='image.relight:0',owner_id=str(uuid5(workflow,'image.relight:0')),reserved_bytes=1000,qualification=pin)
    for label,value in [('params',params),('profile',profile),('inputs',inputs)]:
        body=canonical_bytes(value);request[label+'_jcs']=body.decode();request[label+'_sha']=sha256(body).hexdigest()
    def save(value=request):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            result=db.execute('SELECT photo.save_v28_relight_intent(%s,%s,%s)',(tenant,run,Jsonb(value))).fetchone()[0]
            db.execute('RESET ROLE');return result
    assert save() is True
    assert save() is True
    if verify_depth:
        def origin(body=retained['body'].decode()):
            with db.transaction():
                db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
                db.execute('SET LOCAL ROLE photo_scheduler')
                result=db.execute('SELECT photo.v28_relight_intent_depth_origin(%s,%s,%s)',(tenant,run,body)).fetchone()[0]
                db.execute('RESET ROLE');return result
        original=origin()
        assert original is not None and original['artifact_id']==str(retained['params'])
        assert origin(' '+retained['body'].decode()) is None
        for query,args in (
            ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(retained['task'],)),
            ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pixels'],)),
            ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['invalid'],)),
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(retained['params'],)),
            ('UPDATE photo.runs SET cancel_requested=true WHERE id=(SELECT run_id FROM photo.tasks WHERE id=%s)',(retained['task'],)),
        ):
            with db.transaction(force_rollback=True):db.execute(query,args);assert origin() is None
            assert origin()==original
        origin_sig='photo.v28_relight_intent_depth_origin(uuid,uuid,text)'
        for role in ('photo_scheduler','photo_api','photo_worker','public'):
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,origin_sig)).fetchone()[0] is (role=='photo_scheduler')

    if verify_person:
        def origin(body=retained['body'].decode()):
            with db.transaction():
                db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
                db.execute('SET LOCAL ROLE photo_scheduler')
                result=db.execute('SELECT photo.v28_relight_intent_person_origin(%s,%s,%s)',(tenant,run,body)).fetchone()[0]
                db.execute('RESET ROLE');return result
        original=origin()
        assert original is not None and original['artifact_id']==str(retained['params'])
        assert origin(' '+retained['body'].decode()) is None
        for query,args in (
            ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['root'],)),
            ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
            ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
            ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(retained['task'],)),
            ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pixels'],)),
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(retained['params'],)),
            ('UPDATE photo.runs SET cancel_requested=true WHERE id=(SELECT run_id FROM photo.tasks WHERE id=%s)',(retained['task'],)),
        ):
            with db.transaction(force_rollback=True):db.execute(query,args);assert origin() is None
            assert origin()==original
        origin_sig='photo.v28_relight_intent_person_origin(uuid,uuid,text)'
        for role in ('photo_scheduler','photo_api','photo_worker','public'):
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,origin_sig)).fetchone()[0] is (role=='photo_scheduler')

    altered=deepcopy(request);altered['owner_id']=str(uuid4());assert save(altered) is False
    altered=deepcopy(request);altered['params_sha']='0'*64;assert save(altered) is False
    altered=deepcopy(request);altered['reserved_bytes']=1001;assert save(altered) is False
    for query,args in (
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],)),
    ):
        with db.transaction(force_rollback=True):db.execute(query,args);assert save() is False
        assert save() is True
    assert db.execute('SELECT status,graph_sealed,v28_relight_intent FROM photo.runs WHERE id=%s',(run,)).fetchone()==('draft',False,request)
    assert db.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()==(0,)
    sig='photo.save_v28_relight_intent(uuid,uuid,jsonb)'
    for role in ('photo_scheduler','photo_api','photo_worker','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_scheduler')

    return dict(tenant=tenant,run=run,scope=scope,request=request,retained=retained)


def test_relight_intent_original_person_discovery_and_revocation(db):
    check_relight_intent(db,fixture(db,snapshot_authority=True),verify_person=True)


@pytest.mark.migration_head(997)
def test_relight_intent_person_origin_upgrade(db):
    seed=fixture(db,snapshot_authority=True)
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/00998_v28_relight_intent_person_origin.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    check_relight_intent(db,seed,verify_person=True)


@pytest.mark.parametrize('protected',[False,True])
def test_relight_intent_manual_origin(db,protected):
    saved=check_relight_intent(db,fixture(db,snapshot_authority=True),verify_manual=True)
    tenant,run,retained=saved['tenant'],saved['run'],saved['retained']
    mask=retained['guard_raw'] if protected else retained['target_raw']
    def origin(root=retained['root_raw']):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            value=db.execute('SELECT photo.v28_relight_intent_manual_origin(%s,%s,%s,%s,%s)',(tenant,run,mask.decode(),None if root is None else root.decode(),protected)).fetchone()[0]
            db.execute('RESET ROLE');return value
    actual=origin()
    assert actual is not None and actual['params']['artifact_id']==str(retained['params'])
    assert origin(None)==actual
    assert origin(b' '+retained['root_raw']) is None
    for role in ('mask_target','mask_protected','rasterized_mask'):
        with db.transaction(force_rollback=True):
            db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pins'][role].artifact_id,))
            assert origin() is None
        assert origin()==actual
    signature='photo.v28_relight_intent_manual_origin(uuid,uuid,text,text,boolean)'
    for role in ('photo_scheduler','photo_api','photo_worker','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')


@pytest.mark.migration_head(998)
def test_relight_intent_manual_origin_upgrade(db):
    seed=fixture(db,snapshot_authority=True)
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/00999_v28_relight_intent_manual_origin.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    check_relight_intent(db,seed,verify_manual=True)
    assert db.execute("SELECT to_regprocedure('photo.v28_relight_intent_manual_origin(uuid,uuid,text,text,boolean)')").fetchone()[0] is not None


@pytest.mark.parametrize('protected',[False,True])
def test_relight_intent_manual_pinned_objects(db,protected):
    saved=check_relight_intent(db,fixture(db,snapshot_authority=True),verify_manual=True)
    retained=saved['retained'];tenant=saved['tenant'];run=saved['run']
    mask=retained['guard_raw'] if protected else retained['target_raw']
    def locate(kind,root=retained['root_raw']):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            result=db.execute('SELECT photo.v28_relight_intent_manual_object(%s,%s,%s,%s,%s,%s)',(tenant,run,mask.decode(),None if root is None else root.decode(),protected,kind)).fetchone()[0]
            db.execute('RESET ROLE');return result
    for kind,aid in [('root',retained['root'].artifact_id),('params',retained['params']),('target',retained['pins']['mask_target'].artifact_id),('guard',retained['pins']['mask_protected'].artifact_id)]:
        value=locate(kind)
        assert value is not None and value['artifact_id']==str(aid) and value['object_version']=='v1'
        with db.transaction(force_rollback=True):
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(aid,))
            assert locate(kind) is None
        assert locate(kind)==value
    assert locate('root',None) is not None
    assert locate('target',None) is None
    assert locate('vectors') is None
    assert locate('other') is None
    signature='photo.v28_relight_intent_manual_object(uuid,uuid,text,text,boolean,text)'
    for role in ('photo_scheduler','photo_api','photo_worker','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')


@pytest.mark.migration_head(999)
def test_relight_manual_object_upgrade(db):
    seed=fixture(db,snapshot_authority=True)
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/01000_v28_relight_manual_object.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    saved=check_relight_intent(db,seed,verify_manual=True)
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(saved['tenant']),))
        db.execute('SET LOCAL ROLE photo_scheduler')
        value=db.execute('SELECT photo.v28_relight_intent_manual_object(%s,%s,%s,%s,true,%s)',(saved['tenant'],saved['run'],saved['retained']['guard_raw'].decode(),saved['retained']['root_raw'].decode(),'root')).fetchone()[0]
        db.execute('RESET ROLE')
    assert value is not None and value['artifact_id']==str(saved['retained']['root'].artifact_id)
