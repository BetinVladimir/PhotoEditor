import ssl
from contextlib import contextmanager
from types import SimpleNamespace

import pytest
from photo_worker.operations.learning.installation import LearningEvaluationInstallation
from photo_worker.runtime import service
from test_worker_service import environment


class Provider:
    backend='cuda';renderer_version='v2.2';model_lock_sha='a'*64;icc_lock_sha='b'*64
    @contextmanager
    def render(self,*args):yield None


def test_learning_worker_registration_is_explicit(monkeypatch,tmp_path):
    config=service.WorkerConfig.from_env({**environment(tmp_path),
        'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'cuda'})
    monkeypatch.setattr(service,'client_tls',lambda *a:ssl.create_default_context())
    _,baseline=service.build_worker(config)
    assert 'learning.evaluate' not in baseline.executor.operations
    _,installed=service.build_worker(config,learning_evaluation_installation=
        LearningEvaluationInstallation(Provider(),lambda:None))
    assert installed.executor.operations['learning.evaluate'].resource_class=='gpu'


def test_production_learning_worker_refuses_direct_unsigned_installation(monkeypatch,tmp_path):
    config=service.WorkerConfig.from_env({**environment(tmp_path),
        'APP_ENV':'production', 'PHOTO_WORKER_CLASS':'gpu',
        'PHOTO_WORKER_BACKEND':'cuda'})
    monkeypatch.setattr(service,'client_tls',
                        lambda *a:pytest.fail('TLS before qualification refusal'))
    with pytest.raises(service.WorkerConfigurationError,
                       match='requires signed qualification'):
        service.build_worker(config, learning_evaluation_installation=
            LearningEvaluationInstallation(Provider(), lambda:None))


@pytest.mark.parametrize('resource,backend',[('cpu','cpu'),('gpu','mps'),('media','cpu')])
def test_learning_worker_rejects_wrong_class_before_tls(monkeypatch,resource,backend):
    monkeypatch.setattr(service,'client_tls',lambda *a:pytest.fail('TLS before class validation'))
    with pytest.raises(service.WorkerConfigurationError,match='Learning evaluation'):
        service.build_worker(SimpleNamespace(resource_class=resource,backend=backend),
            learning_evaluation_installation=LearningEvaluationInstallation(Provider(),lambda:None))


def test_run_worker_forwards_learning_installation(monkeypatch,tmp_path):
    marker=LearningEvaluationInstallation(Provider(),lambda:None);calls=[]
    config=service.WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'})
    def build(config,**kwargs):
        calls.append(kwargs);raise RuntimeError('stop before network')
    monkeypatch.setattr(service,'build_worker',build)
    with pytest.raises(RuntimeError,match='stop before network'):
        service.run_worker(config,learning_evaluation_installation=marker)
    assert calls==[{'learning_evaluation_installation':marker}]


def test_worker_registers_composed_native_learning_provider(monkeypatch, tmp_path):
    from photo_worker.operations.learning.installation import pinned_learning_evaluation_installation
    pool = SimpleNamespace(lock=SimpleNamespace(lock_id='a'*64, models=[]))
    registry = SimpleNamespace(resolve=lambda name: SimpleNamespace(sha256='b'*64))
    calls = []
    installation = pinned_learning_evaluation_installation(pool, registry,
        verify_runtime=lambda: calls.append('verify'))
    config = service.WorkerConfig.from_env({**environment(tmp_path),
        'PHOTO_WORKER_CLASS':'gpu', 'PHOTO_WORKER_BACKEND':'cuda'})
    monkeypatch.setattr(service, 'client_tls', lambda *a: ssl.create_default_context())
    _, transport = service.build_worker(config, learning_evaluation_installation=installation)
    assert transport.executor.operations['learning.evaluate'].resource_class == 'gpu'
    assert len(calls) >= 2
