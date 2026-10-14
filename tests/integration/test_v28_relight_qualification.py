from copy import deepcopy
from uuid import uuid4
from psycopg.types.json import Jsonb

def test_relight_qualification_exact_authorized_pin_and_revocation(db):
    from test_v27_semantic_final_producer_gate import fixture
    from semantic_qualification_fixture import frozen_qualification
    tenant,*_,scope=fixture(db,snapshot_authority=True)
    pin=frozen_qualification(db,tenant,scope['project'],scope['actor'],operation='image.relight',backend='cpu')
    pin['policy_sha']=db.execute('SELECT photo.v28_relight_policy_sha(%s)',(pin['policy_id'],)).fetchone()[0]
    assert pin['policy_sha'] is not None
    def lock(value=pin,tid=tenant,project=scope['project'],actor=scope['actor'],session=scope['session']):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            result=db.execute('SELECT photo.lock_v28_relight_qualification(%s,%s,%s,%s,%s)',(tid,project,actor,session,Jsonb(value))).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert lock() is True
    assert lock(tid=uuid4()) is False
    assert lock(project=uuid4()) is False
    assert lock(actor=uuid4()) is False
    assert lock(session=uuid4()) is False
    for field in ('policy_sha','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'):
        altered=deepcopy(pin);altered[field]='0'*64
        assert lock(altered) is False
    for query,args in (
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ('UPDATE photo.v2_qualification_policies SET enabled=false,admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(pin['report']['artifact_id'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(pin['calibration']['artifact_id'],)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,args);assert lock() is False
        assert lock() is True
    altered=deepcopy(pin);altered['qualification_id']=str(uuid4())
    assert lock(altered) is False
    sig='photo.lock_v28_relight_qualification(uuid,uuid,uuid,uuid,jsonb)'
    for role in ('photo_scheduler','photo_worker','photo_api','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_scheduler')


def test_worker_relight_qualification_catalog_acl_and_unowned_refusal(db):
    sig='photo.v28_worker_relight_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    for role in ('photo_worker','photo_scheduler','photo_api','photo_gateway','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_worker')
    row=db.execute('SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure',(sig,)).fetchone()
    assert row==(True,'photo_fn_owner',['search_path=pg_catalog, photo'])
    from hashlib import sha256
    args=(uuid4(),uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(b'{}').hexdigest(),'{}','b'*64)
    assert db.execute('SELECT photo.v28_worker_relight_qualification_evidence('+','.join(['%s']*10)+')',args).fetchone()==(None,)


def test_worker_catalog_positive_exact_locations_and_revocation(db):
    from test_v28_relight_lease import relight_lease_fixture
    args,scope,pin=relight_lease_fixture(db,qualified=True)
    def evidence():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            result=db.execute('SELECT photo.v28_worker_relight_qualification_evidence('+','.join(['%s']*10)+')',args).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    record=evidence()
    assert record is not None and record['pin']==pin and record['policy']['operation']=='image.relight'
    for key in ('report','calibration'):
        location=record[key]
        assert location['artifact_id']==pin[key]['artifact_id'] and location['sha256']==pin[key]['sha256']
        assert location['object_version']=='pinned-v1' and location['bucket']=='fixture'
        assert location['bytes']>=2 and location['media_type']=='application/json'
    for query,values in (
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(args[2],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(pin['report']['artifact_id'],)),
        ("UPDATE photo.tasks SET execution_profile=execution_profile-'relight_qualification' WHERE id=%s",(args[2],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert evidence() is None
        assert evidence()==record
