"""Provider admission scenarios; no model files or GPU needed."""
from types import SimpleNamespace
import pytest
from photo_worker.models.install import ModelError
from photo_worker.models.onnx_session import create_session


@pytest.mark.parametrize('backend', ['cpu', 'cuda'])
def test_declared_provider_and_fallback_policy(backend):
    provider = 'CUDAExecutionProvider' if backend == 'cuda' else 'CPUExecutionProvider'
    entries = {}
    session = SimpleNamespace(get_providers=lambda: [provider])
    session.disable_fallback = lambda: entries.update(disabled=True)
    def construct(path, **kwargs):
        assert path == '/model.onnx'
        assert kwargs['providers'] == [provider]
        assert kwargs['enable_fallback'] is False
        assert ('sess_options' in kwargs) == (backend == 'cuda')
        return session
    ort = SimpleNamespace(get_available_providers=lambda: [provider],
        SessionOptions=lambda: SimpleNamespace(add_session_config_entry=lambda k,v: entries.update({k:v})),
        InferenceSession=construct)
    assert create_session(ort, '/model.onnx', backend) is session
    assert entries['disabled'] is True
    if backend == 'cuda':
        assert entries['session.disable_cpu_ep_fallback'] == '1'


def test_missing_cuda_refused_before_session_creation():
    ort = SimpleNamespace(get_available_providers=lambda: ['CPUExecutionProvider'])
    with pytest.raises(ModelError, match='BACKEND_UNAVAILABLE'):
        create_session(ort, '/model.onnx', 'cuda')


@pytest.mark.parametrize('providers', [['CUDAExecutionProvider', 'CPUExecutionProvider'], ['CPUExecutionProvider']])
def test_runtime_provider_substitution_refused(providers):
    session = SimpleNamespace(get_providers=lambda: providers, disable_fallback=lambda: None)
    ort = SimpleNamespace(get_available_providers=lambda: ['CUDAExecutionProvider'],
        SessionOptions=lambda: SimpleNamespace(add_session_config_entry=lambda *args: None),
        InferenceSession=lambda *args, **kwargs: session)
    with pytest.raises(ModelError, match='BACKEND_UNAVAILABLE'):
        create_session(ort, '/model.onnx', 'cuda')


def test_cuda_initialization_failure_has_no_retry():
    calls = []
    def construct(*args, **kwargs):
        calls.append(kwargs)
        raise RuntimeError('CUDA library unavailable')
    ort = SimpleNamespace(get_available_providers=lambda: ['CUDAExecutionProvider'],
        SessionOptions=lambda: SimpleNamespace(add_session_config_entry=lambda *args: None),
        InferenceSession=construct)
    with pytest.raises(ModelError, match='BACKEND_UNAVAILABLE'):
        create_session(ort, '/model.onnx', 'cuda')
    assert len(calls) == 1 and calls[0]['enable_fallback'] is False
