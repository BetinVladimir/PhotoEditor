"""One declared execution provider; refuse implicit CPU and Python EP fallback."""
from .install import ModelError


def create_session(ort, path, backend):
    if backend not in ('cpu', 'cuda'):
        raise ModelError('BACKEND_UNAVAILABLE')
    provider = 'CUDAExecutionProvider' if backend == 'cuda' else 'CPUExecutionProvider'
    if provider not in ort.get_available_providers():
        raise ModelError('BACKEND_UNAVAILABLE')
    kwargs = {'providers': [provider], 'enable_fallback': False}
    if backend == 'cuda':
        options = ort.SessionOptions()
        options.add_session_config_entry('session.disable_cpu_ep_fallback', '1')
        kwargs['sess_options'] = options
    try:
        session = ort.InferenceSession(str(path), **kwargs)
        session.disable_fallback()
        if session.get_providers() != [provider]:
            raise ModelError('BACKEND_UNAVAILABLE')
        return session
    except ModelError:
        raise
    except Exception as error:
        raise ModelError('BACKEND_UNAVAILABLE') from error
