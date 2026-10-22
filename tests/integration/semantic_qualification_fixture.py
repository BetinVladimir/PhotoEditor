"""Imported qualification metadata fixture, not physical GPU evidence."""
import hashlib
import json
from io import BytesIO
from uuid import uuid4


def frozen_qualification(db, tenant, project, actor, objects=None, calibration_ref=None, model_lock_sha='a'*64, dataset_sha='d'*64, operation='semantic.transfer', backend='cuda', resource_metrics=None,runtime_lock_sha='b'*64, required_checks=None, device_profile_sha='c'*64,evidence_sha='f'*64,existing_policy=None):
    required_checks = ['quality'] if required_checks is None else list(required_checks)
    assert required_checks and len(set(required_checks)) == len(required_checks)
    calibration_sha=calibration_ref['sha256'] if calibration_ref is not None else '6'*64
    db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES(%s,true) ON CONFLICT(operator_id) DO NOTHING', (actor,))
    issuer=actor
    if existing_policy is None:
        policy=db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,license_cleared,enabled,admission_enabled) VALUES(%s,'1',%s,%s,%s,%s,%s,%s,%s,ARRAY[%s],ARRAY[%s],ARRAY[%s],true,true,true) RETURNING id", (operation,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,actor,required_checks,'e'*64,calibration_sha,evidence_sha)).fetchone()[0]
    else:
        assert operation in ('semantic.transfer','semantic.preview_retouch_region')
        from uuid import UUID
        assert type(existing_policy) is dict and set(existing_policy)=={'policy_id','issuer_id','policy_sha'}
        assert all(type(value) is str for value in existing_policy.values())
        policy=UUID(existing_policy['policy_id']);issuer=UUID(existing_policy['issuer_id'])
        assert str(policy)==existing_policy['policy_id'] and str(issuer)==existing_policy['issuer_id']
        fingerprint='photo.v27_semantic_policy_sha' if operation=='semantic.transfer' else 'photo.v27_downstream_policy_sha'
        current=db.execute('SELECT operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,'
            'dataset_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,'
            'license_cleared,enabled,admission_enabled,'+fingerprint+'(id) '
            'FROM photo.v2_qualification_policies WHERE id=%s FOR SHARE',(policy,)).fetchone()
        assert current == (operation,'1',model_lock_sha,runtime_lock_sha,backend,device_profile_sha,
            dataset_sha,issuer,required_checks,['e'*64],[calibration_sha],[evidence_sha],
            True,True,True,existing_policy['policy_sha']), 'qualification reuse requires exact current policy'
        # Policy issuer is global; importer remains this tenant's own owner/operator.
        # Do not enable a revoked shared issuer or change policy rows on reuse.
        assert db.execute('SELECT operator_id FROM photo.v2_qualification_operators '
            'WHERE operator_id=%s AND enabled FOR SHARE',(issuer,)).fetchone() == (issuer,)
    report=dict(schema_version=1,operation=operation,version='1',model_lock_sha=model_lock_sha,runtime_lock_sha=runtime_lock_sha,device_profile_sha=device_profile_sha,dataset_sha=dataset_sha,input_fixture_shas=['e'*64],checks=[dict(id=check,passed=True,evidence_sha=evidence_sha) for check in required_checks],metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=0,peak_rss_bytes=0,scratch_bytes=0),status='passed',issuer_id=str(issuer),created_at='2026-09-01T00:00:00Z')
    if resource_metrics is not None:
        report['metrics'].update(resource_metrics)
    body=json.dumps(report,sort_keys=True,separators=(',',':')).encode();sha=hashlib.sha256(body).hexdigest()
    def artifact(digest,size,raw=None):
        aid=uuid4()
        bucket,key,version='fixture',str(aid),'pinned-v1'
        if objects is not None and raw is not None:
            client,bucket=objects
            version=client.put_object(bucket,key,BytesIO(raw),len(raw),content_type='application/json',metadata={'sha256':digest}).version_id
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp())", (aid,tenant,bucket,key,version,digest,size))
        return dict(artifact_id=str(aid),sha256=digest)
    report_ref=artifact(sha,len(body),body);calibration=calibration_ref if calibration_ref is not None else artifact(calibration_sha,2)
    db.execute('SET LOCAL ROLE photo_maintenance')
    try:
        qualification=db.execute('SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)', (tenant,project,report_ref['artifact_id'],sha,actor,backend,body.decode())).fetchone()[0]
    finally:db.execute('RESET ROLE')
    fingerprint='photo.v27_semantic_policy_sha' if operation=='semantic.transfer' else 'photo.v27_downstream_policy_sha'
    policy_sha=db.execute('SELECT '+fingerprint+'(%s)',(policy,)).fetchone()[0]
    return dict(policy_id=str(policy),qualification_id=str(qualification),policy_sha=policy_sha,report=report_ref,calibration=calibration,model_lock_sha=model_lock_sha,runtime_lock_sha=runtime_lock_sha,device_profile_sha=device_profile_sha,dataset_sha=dataset_sha,issuer_id=str(issuer),backend=backend)


def qualification_policy_for(policies,operation):
    """Closed opt-in test policy reuse; tenant qualifications are never reused."""
    if policies is None:
        return None
    assert type(policies) is dict and set(policies)=={'semantic.transfer','semantic.preview_retouch_region'}
    assert operation in policies
    for descriptor in policies.values():
        assert type(descriptor) is dict and set(descriptor)=={'policy_id','issuer_id','policy_sha'}
    return dict(policies[operation])
