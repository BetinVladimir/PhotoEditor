from types import SimpleNamespace
import pytest
from photo_worker.operations.optical.multi_admission import PinnedMultiImageAdmission
from photo_worker.runtime.executor import OperationFailure


def test_installation_rejects_unknown_operation_before_runtime_lookup():
    with pytest.raises(ValueError,match='MULTI_OPERATION_INVALID'):
        PinnedMultiImageAdmission(operation='optical.unknown',runtime_lock={},runtime_sha='a'*64,
            pin={},policy={},max_pixels=100,resolve_evidence=lambda *args:None,verify_color=lambda _:True)


def test_admission_rejects_missing_ordered_source_before_runtime_lookup():
    # Uninstalled instance exercises the pre-runtime source guard only.
    admission=object.__new__(PinnedMultiImageAdmission)
    admission.operation='optical.panorama'
    from test_multi_request import fixture
    params,_,_=fixture()
    with pytest.raises(OperationFailure,match='MULTI_SOURCE_COUNT_INVALID'):
        admission.admit(SimpleNamespace(),params,[])


def installed(monkeypatch,operation='optical.panorama'):
    from copy import deepcopy
    from uuid import uuid4
    from test_relight_qualification import evidence
    from test_perspective_adapter import source
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
    from photo_worker.contracts.optical_multi import OpticalPanoramaParams,OpticalFocusStackParams
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.operations.optical import multi_admission as module
    from photo_worker.operations.optical.multi_runtime_lock import MultiImageRuntimeLock,component_paths,PACKAGES
    _,pin,report,calibration,policy=evidence()
    lock=MultiImageRuntimeLock(schema_version=1,python_version='3.12.0',implementation='CPython',machine='test',
        packages={name:'test' for name in PACKAGES},components={key:'a'*64 for key in component_paths()})
    runtime=canonical_hash(lock)
    policy.update(operation=operation,runtime_lock_sha=runtime)
    report.update(operation=operation,runtime_lock_sha=runtime)
    pin.update(runtime_lock_sha=runtime,policy_sha=canonical_hash(policy))
    pin['report']['sha256']=canonical_hash(report)
    images=[source(),source()]
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(image)),
        canvas_hash=canonical_hash(image.canvas)) for image in images]
    model=OpticalPanoramaParams if operation=='optical.panorama' else OpticalFocusStackParams
    intent=model(schema_version=1,frames=bindings,reference_ordinal=1,max_pixels=10000,
        calibration=ArtifactRef.model_validate_json(canonical_bytes(pin['calibration'])),**({'projection':'planar'} if operation=='optical.panorama' else {'motion_policy':'review'}))
    record=module.MultiImageEvidence(pin,policy,canonical_bytes(report),canonical_bytes(calibration),operation,canonical_bytes(intent))
    records=[record];calls=[];colors=[]
    def resolve(*args):
        calls.append(True)
        return deepcopy(records[-1])
    monkeypatch.setattr(module,'verify_installed_runtime',lambda *args:runtime)
    admission=PinnedMultiImageAdmission(operation=operation,runtime_lock=lock,runtime_sha=runtime,pin=pin,policy=policy,
        max_pixels=10000,resolve_evidence=resolve,verify_color=lambda value:(colors.append(value) or True))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={
        'resource_estimate':dict(ram_bytes=1<<30,scratch_bytes=1<<30)}))
    return admission,ctx,intent,images,records,calls,colors


@pytest.mark.parametrize('operation',['optical.panorama','optical.focus_stack'])
def test_exact_report_ordered_sources_and_live_grant(monkeypatch,operation):
    from photo_worker.contracts.base import canonical_bytes
    admission,ctx,intent,images,records,calls,colors=installed(monkeypatch,operation)
    request=admission.admit(ctx,intent,images)
    assert request.operation==operation and request.params_jcs==canonical_bytes(intent)
    assert request.calibration_jcs==records[0].calibration_raw
    assert len(calls)==2 and colors==[image.color for image in images]


@pytest.mark.parametrize('change',['order','report','calibration','scope','operation','budget','icc','revoked'])
def test_multi_admission_refuses_substitution(monkeypatch,change):
    from dataclasses import replace
    admission,ctx,intent,images,records,calls,colors=installed(monkeypatch)
    if change=='order':images.reverse()
    if change=='report':records[0]=replace(records[0],report_raw=b' '+records[0].report_raw)
    if change=='calibration':records[0]=replace(records[0],calibration_raw=b' '+records[0].calibration_raw)
    if change=='scope':records[0]=replace(records[0],params_jcs=b'{}')
    if change=='operation':records[0]=replace(records[0],operation='optical.focus_stack')
    if change=='budget':ctx.lease.execution_profile['resource_estimate']['ram_bytes']=0
    if change=='icc':admission.verify_color=lambda _:False
    if change=='revoked':
        original=admission.resolve_evidence
        def revoked(*args):
            result=original(*args)
            return replace(result,params_jcs=b'{}') if len(calls)>1 else result
        admission.resolve_evidence=revoked
    with pytest.raises((ValueError,OperationFailure)):
        admission.admit(ctx,intent,images)
