"""Imported qualification metadata fixture, not physical GPU evidence."""
import hashlib
import json
from io import BytesIO
from uuid import uuid4


def frozen_qualification(db, tenant, project, actor, objects=None, calibration_ref=None, model_lock_sha='a'*64, dataset_sha='d'*64, operation='semantic.transfer', backend='cuda', resource_metrics=None,runtime_lock_sha='b'*64):
    calibration_sha=calibration_ref['sha256'] if calibration_ref is not None else '6'*64
    db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES(%s,true) ON CONFLICT(operator_id) DO NOTHING', (actor,))
    policy=db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,license_cleared,enabled,admission_enabled) VALUES(%s,'1',%s,%s,%s,%s,%s,%s,ARRAY['quality'],ARRAY[%s],ARRAY[%s],ARRAY[%s],true,true,true) RETURNING id", (operation,model_lock_sha,runtime_lock_sha,backend,'c'*64,dataset_sha,actor,'e'*64,calibration_sha,'f'*64)).fetchone()[0]
    report=dict(schema_version=1,operation=operation,version='1',model_lock_sha=model_lock_sha,runtime_lock_sha=runtime_lock_sha,device_profile_sha='c'*64,dataset_sha=dataset_sha,input_fixture_shas=['e'*64],checks=[dict(id='quality',passed=True,evidence_sha='f'*64)],metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=0,peak_rss_bytes=0,scratch_bytes=0),status='passed',issuer_id=str(actor),created_at='2026-09-01T00:00:00Z')
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
    return dict(policy_id=str(policy),qualification_id=str(qualification),policy_sha=policy_sha,report=report_ref,calibration=calibration,model_lock_sha=model_lock_sha,runtime_lock_sha=runtime_lock_sha,device_profile_sha='c'*64,dataset_sha=dataset_sha,issuer_id=str(actor),backend=backend)
