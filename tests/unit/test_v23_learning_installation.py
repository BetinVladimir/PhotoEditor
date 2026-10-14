from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace

import pytest

from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.learning.installation import (
    LearningEvaluationInstallation, VerifiedLearningProvider,
    installed_learning_evaluate_operations)
from photo_worker.operations.learning.qualification import LearningQualification


class Provider:
    backend='cuda'
    renderer_version='v2.2'
    model_lock_sha='a'*64
    icc_lock_sha='b'*64
    def __init__(self,mode='valid'):
        self.mode=mode;self.events=[]
    @contextmanager
    def render(self,*args):
        self.events.append('open')
        try:
            if self.mode=='open':self.model_lock_sha='c'*64
            yield object()
        finally:
            self.events.append('close')
            if self.mode=='close':self.icc_lock_sha='c'*64


@pytest.mark.parametrize('mode',['valid','open','close','verify','before'])
def test_learning_installation_pins_runtime_through_provider_scope(mode):
    provider=Provider(mode);checks=[]
    installed=VerifiedLearningProvider(LearningEvaluationInstallation(provider,lambda:checks.append('verify')))
    if mode=='verify':
        installed._verify=lambda:setattr(provider,'renderer_version','changed')
    if mode=='before':provider.backend='mps'
    ctx=SimpleNamespace(check_active=lambda:None)
    def run():
        with installed.render(ctx,None,None,None,None):pass
    if mode=='valid':run()
    else:
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):run()
    assert provider.events==([] if mode in ('before','verify') else ['open','close'])
    assert checks


@pytest.mark.parametrize('mode',['backend','renderer','model','icc','callback','result'])
def test_learning_installation_refuses_unqualified_shape(mode):
    provider=Provider();verify=lambda:None
    if mode=='backend':provider.backend='mps'
    if mode=='renderer':provider.renderer_version='v1'
    if mode=='model':provider.model_lock_sha='bad'
    if mode=='icc':provider.icc_lock_sha=None
    if mode=='callback':verify=None
    if mode=='result':verify=lambda:False
    with pytest.raises(ValueError,match='LEARNING_'):
        VerifiedLearningProvider(LearningEvaluationInstallation(provider,verify))


def test_installed_learning_operation_checks_runtime_before_artifact_creation(monkeypatch):
    import photo_worker.operations.learning.evaluate_adapter as adapter
    provider=Provider();installed=LearningEvaluationInstallation(provider,lambda:None)
    operations=installed_learning_evaluate_operations(None,installed)
    assert [(op.name,op.resource_class) for op in operations]==[('learning.evaluate','gpu')]
    monkeypatch.setattr(adapter.TaskArtifacts,'create',lambda *a:pytest.fail('artifact IO after changed provider'))
    provider.model_lock_sha='c'*64
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        operations[0].execute(SimpleNamespace(check_active=lambda:None))


@pytest.mark.parametrize('failure', [OSError('profile disappeared'),
                                      RuntimeError('CUDA unavailable')])
def test_learning_runtime_dependency_failure_is_config_invalid(failure):
    provider = Provider()
    installed = VerifiedLearningProvider(LearningEvaluationInstallation(provider, lambda: None))
    installed._verify = lambda: (_ for _ in ()).throw(failure)
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        with installed.render(SimpleNamespace(check_active=lambda: None), None, None, None, None):
            pytest.fail('render started after runtime qualification failure')
    assert provider.events == []


def test_signed_qualification_limits_reviewed_effect_before_render():
    provider = Provider()
    proof = LearningQualification(provider.model_lock_sha, provider.icc_lock_sha,
        'c' * 64, ('RTX 3060', 8, 6, 12_000_000_000), frozenset({'color'}),
        10_000_000_000, datetime.now(timezone.utc) + timedelta(days=1), 'd' * 64)
    installed = VerifiedLearningProvider(LearningEvaluationInstallation(
        provider, lambda: None, proof))
    extension = SimpleNamespace(region_adjustments=[], background=None, restoration=None)
    arm = SimpleNamespace(local=SimpleNamespace(enabled=False),
        beauty=SimpleNamespace(enabled=False), defects=SimpleNamespace(enabled=True),
        extensions=extension)
    styles = SimpleNamespace(baseline=arm, candidate=arm, approved=arm)
    ctx = SimpleNamespace(check_active=lambda: None,
        lease=SimpleNamespace(execution_profile={
            'resource_estimate': {'vram_bytes': 10_000_000_000}}))
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        with installed.render(ctx, None, None, styles, None):
            pytest.fail('unqualified reviewed repair rendered')
    assert provider.events == []


def test_signed_color_only_qualification_refuses_reviewed_dust_before_render():
    provider = Provider()
    proof = LearningQualification(provider.model_lock_sha, provider.icc_lock_sha,
        'c' * 64, ('RTX 3060', 8, 6, 12_000_000_000), frozenset({'color'}),
        10_000_000_000, datetime.now(timezone.utc) + timedelta(days=1), 'd' * 64)
    installed = VerifiedLearningProvider(LearningEvaluationInstallation(
        provider, lambda: None, proof))
    restoration = SimpleNamespace(denoise='off', upscale='off',
                                  sensor_dust='review')
    extension = SimpleNamespace(region_adjustments=[], background=None,
                                restoration=restoration)
    arm = SimpleNamespace(local=SimpleNamespace(enabled=False),
        beauty=SimpleNamespace(enabled=False), defects=SimpleNamespace(enabled=False),
        extensions=extension)
    styles = SimpleNamespace(baseline=arm, candidate=arm, approved=arm)
    ctx = SimpleNamespace(check_active=lambda: None,
        lease=SimpleNamespace(execution_profile={
            'resource_estimate': {'vram_bytes': 10_000_000_000}}))
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        with installed.render(ctx, None, None, styles, None):
            pytest.fail('unsigned reviewed dust rendered')
    assert provider.events == []


def test_signed_qualification_refuses_underfunded_gpu_task():
    provider = Provider()
    proof = LearningQualification(provider.model_lock_sha, provider.icc_lock_sha,
        'c' * 64, ('RTX 3060', 8, 6, 12_000_000_000), frozenset({'color'}),
        10_000_000_000, datetime.now(timezone.utc) + timedelta(days=1), 'd' * 64)
    installed = VerifiedLearningProvider(LearningEvaluationInstallation(
        provider, lambda: None, proof))
    ctx = SimpleNamespace(check_active=lambda: None,
        lease=SimpleNamespace(execution_profile={
            'resource_estimate': {'vram_bytes': 4_294_967_296}}))
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        installed.verify_for_task(ctx)
