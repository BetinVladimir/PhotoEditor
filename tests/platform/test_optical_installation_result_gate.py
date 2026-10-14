"""Mocked installation boundaries; no native/photographic qualification."""
from contextlib import contextmanager
from types import SimpleNamespace
import importlib
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('kind,operation', [('perspective','optical.perspective'),('multi','optical.panorama'),('multi','optical.focus_stack')])
@pytest.mark.parametrize('drift',[False,True])
@pytest.mark.parametrize('invalid_profile',[False,True])
def test_runtime_is_rechecked_before_returning_optical_result(monkeypatch,kind,operation,drift,invalid_profile):
    module=importlib.import_module('photo_worker.operations.optical.'+kind+'_installation')
    adapter=importlib.import_module('photo_worker.operations.optical.'+kind+'_adapter')
    artifacts=importlib.import_module('photo_worker.runtime.artifacts')
    events=[]
    class Lock:
        @staticmethod
        def model_validate_json(raw):return {'schema_version':1}
    monkeypatch.setattr(module,'PerspectiveRuntimeLock' if kind=='perspective' else 'MultiImageRuntimeLock',Lock)
    def verify(*args):
        events.append('verify')
        if drift and 'execute' in events:raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    monkeypatch.setattr(module,'verify_installed_runtime',verify)
    monkeypatch.setattr(module,'PinnedPerspectiveAdmission' if kind=='perspective' else 'PinnedMultiImageAdmission',lambda **kwargs:object())
    monkeypatch.setattr(module,kind+'_evidence_resolver',lambda *args:object())
    @contextmanager
    def create(*args):
        events.append('open')
        try:yield object()
        finally:events.append('close')
    monkeypatch.setattr(artifacts.TaskArtifacts,'create',create)
    def execute(*args,**kwargs):events.append('execute');return 'produced'
    monkeypatch.setattr(adapter,'execute',execute)
    policy=dict(operation=operation,operation_version='1',backend='cpu',license_cleared=True)
    options_class=module.PerspectiveRuntimeOptions if kind=='perspective' else module.MultiImageRuntimeOptions
    values=dict(runtime_lock={'schema_version':1},runtime_sha='a'*64,pin={'policy_sha':canonical_hash(policy)},policy=policy,max_pixels=100)
    if kind=='multi':values['operation']=operation
    installation_class=module.PerspectiveInstallation if kind=='perspective' else module.MultiImageInstallation
    installation=installation_class(SimpleNamespace(get_object=lambda *args:None),'photos',options_class(**values))
    db=SimpleNamespace(**{kind+'_qualification_evidence':lambda *args:None,kind+'_capabilities_ready':lambda:True})
    factory=module.perspective_operations if kind=='perspective' else module.multi_operations
    installed=factory(None,db,installation,verify_color=lambda *args:None)[0]
    profile={('perspective_runtime_sha' if kind=='perspective' else 'multi_runtime_sha'):'a'*64}
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=SimpleNamespace(operation=operation,operation_version='1',resource_class='cpu'),execution_profile=profile))
    if invalid_profile:
        ctx.lease.execution_profile=None
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):installed.execute(ctx)
        assert events==['verify']
        return
    if drift:
        with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):installed.execute(ctx)
    else:assert installed.execute(ctx)=='produced'
    assert events==['verify','verify','open','execute','verify','close']
