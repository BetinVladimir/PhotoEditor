"""Synthetic qualification/installation scenarios; no photographic quality claim."""
from copy import deepcopy
from dataclasses import asdict,replace
from hashlib import sha256
from io import BytesIO
import json
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash,ArtifactRef as ContractRef
from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef
from photo_worker.v2.shown_retouch_qualification import ShownRetouchQualificationPin,verify_shown_retouch_qualification_report
from photo_worker.operations.semantic import preview_retouch_adopt_installation as module
from test_semantic_qualification_report import fixture as report_fixture
from test_v27_shown_retouch_task_fixture import fixture as params_fixture


def copy_report_fixture():
    policy,report,_,pin=report_fixture()
    policy.update(operation='semantic.adopt_shown_retouch_region',backend='cpu')
    report['operation']='semantic.adopt_shown_retouch_region'
    raw=canonical_bytes(report);fields=asdict(pin)
    fields.update(backend='cpu',policy_sha=canonical_hash(policy),calibration=pin.calibration,
        report=ArtifactRef(pin.report.artifact_id,sha256(raw).hexdigest()))
    return policy,report,raw,ShownRetouchQualificationPin(**fields)


def test_shown_retouch_report_accepts_only_own_cpu_operation():
    policy,report,raw,pin=copy_report_fixture()
    assert verify_shown_retouch_qualification_report(raw,pin,policy,check_active=lambda:None)==report
    for operation in ('semantic.preview_retouch_region','semantic.adopt_shown_background_cleanup','semantic.local_exposure'):
        changed=dict(report,operation=operation);body=canonical_bytes(changed)
        changed_pin=replace(pin,report=ArtifactRef(pin.report.artifact_id,sha256(body).hexdigest()))
        with pytest.raises(AdoptionInvalid):
            verify_shown_retouch_qualification_report(body,changed_pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('mode',[None,'revoked_report','short_report','version','admission_lost','lease_lost'])
@pytest.mark.parametrize('transport_only',[False,True])
def test_shown_retouch_installer_reloads_real_report_verifier_every_runtime_check(monkeypatch,mode,transport_only):
    policy,_,report,pin=copy_report_fixture()
    value=params_fixture()
    for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha'):value[key]=getattr(pin,key)
    params=ShownRetouchAdoptionInput.model_validate_json(canonical_bytes(json.loads(json.dumps(value,default=str))));raw=canonical_bytes(params)
    pin_value=json.loads(json.dumps(asdict(pin),default=str))
    profile=dict(backend='cpu',renderer_version='v2.2',qualification=pin_value)
    refs=[params.approved_edit,params.presentation,params.numerical_evidence,params.working_image,params.pixels]
    command=SimpleNamespace(operation='semantic.adopt_shown_retouch_region',operation_version='1',resource_class='cpu',
        tenant_id=params.tenant_id,run_id=params.child_run_id,inputs=refs,
        params=ContractRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest()))
    if transport_only:command.inputs=[command.params]
    lease=SimpleNamespace(command=command,execution_profile=profile,fence=7)
    ctx=SimpleNamespace(lease=deepcopy(lease),check_active=lambda:None)
    record=dict(pin=pin_value,policy=policy,report=dict(artifact_id=str(pin.report.artifact_id),sha256=pin.report.sha256,
        bucket='photos',object_key='qualification',object_version='null' if mode=='version' else 'v1',bytes=len(report),media_type='application/json'))
    evidence_calls=[];admissions=[];responses=[];gets=[]
    def evidence(owner,body):
        assert owner==lease and owner is not ctx.lease and body==raw
        evidence_calls.append(owner)
        if mode=='revoked_report' and len(evidence_calls)>1:return None
        return deepcopy(record)
    def admission(owner,body):
        assert owner==lease and body==raw
        admissions.append(owner)
        return mode!='admission_lost'
    def get(bucket,key,*,version_id):
        gets.append((bucket,key,version_id))
        response=BytesIO(report[:-1] if mode=='short_report' else report)
        responses.append(response)
        return response
    monkeypatch.setattr(module,'local_exposure_icc_verifier',lambda registry:lambda color:True)
    factory=module.sql_shown_retouch_factory(database=SimpleNamespace(
        shown_retouch_report_evidence=evidence,admit_semantic_shown_retouch=admission),
        objects=SimpleNamespace(get_object=get),bucket='photos',icc_registry=object(),
        **{key:getattr(pin,key) for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    def install():return factory(ctx,SimpleNamespace(contract_bytes=lambda *args:raw,declared_input_refs=lambda **kwargs:tuple(refs)))
    if mode not in (None,'lease_lost'):
        with pytest.raises(AdoptionInvalid):install()
    else:
        installed=install()
        assert len(evidence_calls)==2 and len(admissions)==1
        if mode=='lease_lost':
            ctx.lease.fence+=1
            with pytest.raises(AdoptionInvalid,match='LEASE_CHANGED'):
                installed.verify_runtime(params,profile)
            assert len(gets)==1
        else:
            assert installed.verify_runtime(params,deepcopy(profile)) is True
            assert installed.verify_admission(deepcopy(lease),raw) is True
            assert len(evidence_calls)==4 and len(admissions)==2
            assert gets==[('photos','qualification','v1')]*2
    assert all(response.closed for response in responses)
    if mode=='version':assert not gets and not admissions
