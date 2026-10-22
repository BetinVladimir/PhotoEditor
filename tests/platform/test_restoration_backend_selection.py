"""Authored backend selection scenarios; no model loading or GPU execution."""
import pytest
from photo_worker.runtime.service import _select_restoration_backend


def test_metal_omits_all_unsupported_restoration_providers():
    providers = (lambda *_: None, object(), lambda *_: None, object())
    assert _select_restoration_backend('mps', *providers) == (None, None, None, None)


@pytest.mark.parametrize('backend', ['cpu', 'cuda'])
def test_supported_backend_retains_exact_providers_for_normal_validation(backend):
    providers = (lambda *_: None, object(), lambda *_: None, object())
    selected = _select_restoration_backend(backend, *providers)
    assert all(actual is expected for actual, expected in zip(selected, providers))


def test_absent_providers_remain_absent_on_metal():
    assert _select_restoration_backend('mps', None, None, None, None) == (None,) * 4
