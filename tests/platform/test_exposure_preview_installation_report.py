"""Preview installer uses actual report bytes and rechecks live evidence."""
from copy import deepcopy
from dataclasses import asdict
from hashlib import sha256
from io import BytesIO
import json
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput
from photo_worker.operations.semantic import preview_installation as module
from photo_worker.v2.adoption import AdoptionInvalid
from test_exposure_qualification_report import exposure_fixture
from test_v27_exposure_preview_binding import fixture


@pytest.mark.parametrize('failure',[None,'revoked','short','denied','nonboolean','lease_changed'])
def test_preview_installer_loads_report_before_sql_admission(monkeypatch,failure):
    policy,_,report,pin=exposure_fixture()
    params,_,_=fixture()
    params.update(model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,
                  device_profile_sha=pin.device_profile_sha)
    value=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    raw=canonical_bytes(value)
    command=SimpleNamespace(operation='semantic.preview_local_exposure',operation_version='1',
        resource_class='cpu',tenant_id=value.tenant_id,
        params=SimpleNamespace(sha256=sha256(raw).hexdigest()),
        inputs=[value.plan,value.intent,value.candidate,value.source.source,value.protection])
    record=dict(pin=json.loads(json.dumps(asdict(pin),default=str)),policy=policy,
        report=dict(artifact_id=str(pin.report.artifact_id),sha256=pin.report.sha256,
            bucket='owned',object_key='report',object_version='v1',bytes=len(report),media_type='application/json'))
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,fence=17,
        execution_profile=dict(backend='cpu',renderer_version='v2.2',qualification=record['pin'])),
        check_active=lambda:None)
    calls=[];responses=[]
    def evidence(owner,body):
        assert owner.fence==17 and body==raw
        calls.append('evidence')
        return None if failure=='revoked' and calls.count('evidence')==2 else deepcopy(record)
    def get(bucket,key,*,version_id):
        assert (bucket,key,version_id)==('owned','report','v1')
        calls.append('object')
        if failure=='lease_changed':ctx.lease.fence=18
        response=BytesIO(report[:-1] if failure=='short' else report)
        responses.append(response);return response
    def admit(owner,body):
        assert owner.fence==17 and body==raw
        calls.append('admit')
        return False if failure=='denied' else (1 if failure=='nonboolean' else True)
    monkeypatch.setattr(module,'local_exposure_icc_verifier',lambda registry:lambda color:True)
    factory=module.sql_exposure_preview_factory(database=SimpleNamespace(
        admit_semantic_exposure_preview=admit,exposure_preview_report_evidence=evidence,
        exposure_preview_protection_origin=lambda *args:True),objects=SimpleNamespace(get_object=get),
        bucket='owned',icc_registry=object(),model_lock_sha=pin.model_lock_sha,
        runtime_lock_sha=pin.runtime_lock_sha,device_profile_sha=pin.device_profile_sha,dataset_sha=pin.dataset_sha)
    artifacts=SimpleNamespace(contract_bytes=lambda *args:raw)
    if failure:
        with pytest.raises(AdoptionInvalid):factory(ctx,artifacts)
    else:
        installed=factory(ctx,artifacts)
        assert installed.verify_admission(deepcopy(ctx.lease),raw) is True
    assert all(response.closed for response in responses)
    if failure in ('revoked','short','lease_changed'):assert 'admit' not in calls
    else:assert calls[:4]==['evidence','object','evidence','admit']
