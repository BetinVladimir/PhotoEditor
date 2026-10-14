"""Trusted-import metadata proof; not a real downstream model qualification."""
from copy import deepcopy
from uuid import uuid4
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


def test_exact_qualification_pin_and_revocations(db):
    tid,_,_,_,_,_,scope=fixture(db,snapshot_authority=True)
    pin=scope['qualification']
    def ready(value=pin,operation='semantic.transfer',backend='cuda',project=scope['project']):
        return db.execute('SELECT photo.v27_downstream_qualification_pin_ready(%s,%s,%s,%s,%s,%s)',(tid,project,operation,'1',backend,Jsonb(value))).fetchone()[0]
    assert ready() is True
    assert not ready(operation='semantic.local_exposure')
    assert not ready(backend='cpu')
    assert not ready(project=uuid4())
    for key in ('policy_id','qualification_id','policy_sha','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id','backend'):
        changed=deepcopy(pin);changed[key]=str(uuid4()) if key.endswith('_id') else '0'*64
        assert not ready(changed)
    for query,values in (
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ('UPDATE photo.v2_qualification_policies SET license_cleared=false,admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(pin['calibration']['artifact_id'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert not ready()
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_downstream_qualification_pin_ready(uuid,uuid,text,text,text,jsonb)','EXECUTE')",(role,)).fetchone()[0]


def test_qualification_authority_locks_concurrent_revocation(database):
    import psycopg
    import pytest
    with psycopg.connect(**database) as setup:
        tid,_,_,_,_,_,scope=fixture(setup,snapshot_authority=True)
        pin=scope['qualification']
    with psycopg.connect(**database) as verifier,psycopg.connect(**database) as competing:
        def ready():
            return verifier.execute('SELECT photo.v27_downstream_qualification_pin_ready(%s,%s,%s,%s,%s,%s)',(tid,scope['project'],'semantic.transfer','1','cuda',Jsonb(pin))).fetchone()[0]
        assert ready() is True
        for query,values in (
            ("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(tid,)),
            ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(scope['project'],)),
            ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(pin['calibration']['artifact_id'],)),
        ):
            with competing.transaction(force_rollback=True):
                competing.execute("SET LOCAL lock_timeout='100ms'")
                with pytest.raises(psycopg.errors.LockNotAvailable):competing.execute(query,values)
        verifier.commit()
        with competing.transaction():
            competing.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        assert ready() is False


def test_downstream_profile_must_match_registry_and_qualification(db):
    tid,_,_,_,_,_,scope=fixture(db,snapshot_authority=True)
    pin=scope['qualification']
    profile=dict(qualification=pin,renderer_version='v2.2',backend=pin['backend'],**{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    def ready(value=profile,operation='semantic.transfer',version='1',resource_class='gpu'):
        return db.execute('SELECT photo.v27_downstream_profile_ready(%s,%s,%s,%s,%s,%s)',(tid,scope['project'],operation,version,resource_class,Jsonb(value))).fetchone()[0]
    assert ready() is True
    for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','renderer_version','backend','qualification'):
        changed=deepcopy(profile);changed[key]={} if key=='qualification' else 'cpu' if key=='backend' else '0'*64
        assert not ready(changed)
    assert not ready(operation='semantic.local_exposure',resource_class='cpu')
    assert not ready(version='2')
    assert not ready(resource_class='cpu')
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_downstream_profile_ready(uuid,uuid,text,text,text,jsonb)','EXECUTE')",(role,)).fetchone()[0]
