from copy import deepcopy
from dataclasses import replace
from types import SimpleNamespace
import pytest
from test_relight_qualification import evidence
from test_perspective_adapter import source,params
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.operations.optical import perspective_admission as module
from photo_worker.operations.optical.perspective_runtime_lock import PerspectiveRuntimeLock,component_paths
from photo_worker.operations.optical.perspective_admission import PinnedPerspectiveAdmission,PerspectiveEvidence
from photo_worker.runtime.executor import OperationFailure


def setup_admission(monkeypatch):
    _,pin,report,calibration,policy=evidence()
    lock=PerspectiveRuntimeLock(schema_version=1,python_version='3.12.0',implementation='CPython',machine='test',
        packages={'numpy':'test','pydantic':'test','rfc8785':'test'},components={key:'a'*64 for key in component_paths()})
    runtime=canonical_hash(lock)
    policy.update(operation='optical.perspective',runtime_lock_sha=runtime)
    report.update(operation='optical.perspective',runtime_lock_sha=runtime)
    pin.update(runtime_lock_sha=runtime,policy_sha=canonical_hash(policy))
    pin['report']['sha256']=canonical_hash(report)
    image=source();intent=params(image,1)
    record=PerspectiveEvidence(pin,policy,canonical_bytes(report),canonical_bytes(calibration),
        intent.base.asset_version_id,intent.base.source,intent.base.canvas_hash,intent.lines)
    records=[record];calls=[]
    def resolve(*args):calls.append(True);return deepcopy(records[-1])
    monkeypatch.setattr(module,'verify_installed_runtime',lambda *args:runtime)
    colors=[]
    admission=PinnedPerspectiveAdmission(runtime_lock=lock,runtime_sha=runtime,pin=pin,policy=policy,
        max_pixels=100000,resolve_evidence=resolve,verify_color=lambda value:(colors.append(value) or True))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={
        'resource_estimate':dict(ram_bytes=1<<30,scratch_bytes=1<<30)}))
    return admission,ctx,intent,image,records,calls,colors


def test_original_report_and_live_grant_rechecked(monkeypatch):
    admission,ctx,intent,image,records,calls,colors=setup_admission(monkeypatch)
    assert admission.admit(ctx,intent,image,None)==100000
    assert len(calls)==2 and colors==[image.color]


@pytest.mark.parametrize('change',['source','lines','report','policy','calibration','budget'])
def test_substitution_and_resource_failure(monkeypatch,change):
    admission,ctx,intent,image,records,calls,colors=setup_admission(monkeypatch)
    record=records[0]
    if change=='source':record=replace(record,canvas_hash='0'*64)
    if change=='lines':record=replace(record,lines=intent.base.source)
    if change=='report':record=replace(record,report_raw=b' '+record.report_raw)
    if change=='policy':record=replace(record,policy={**record.policy,'license_cleared':False})
    if change=='calibration':record=replace(record,calibration_raw=b' '+record.calibration_raw)
    if change=='budget':ctx.lease.execution_profile['resource_estimate']['ram_bytes']=0
    records[0]=record
    with pytest.raises((ValueError,OperationFailure)):admission.admit(ctx,intent,image,None)


def test_false_icc_result_cannot_authorize_execution(monkeypatch):
    admission,ctx,intent,image,records,calls,colors=setup_admission(monkeypatch)
    admission.verify_color=lambda value:False
    with pytest.raises(OperationFailure,match='ICC_MISMATCH'):admission.admit(ctx,intent,image,None)
