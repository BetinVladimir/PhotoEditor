"""Startup must never silently enable or misroute the shown-copy operation."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime.shown_removal_config import shown_removal_from_environment
from photo_worker.runtime.service import WorkerConfigurationError


def test_shown_removal_copy_disabled_without_reading_runtime_configuration():
    assert shown_removal_from_environment(SimpleNamespace(), lambda key, default='': default) is None


@pytest.mark.parametrize('enabled', ['', 'TRUE', '1', 'yes'])
def test_shown_removal_copy_enablement_is_explicit(enabled):
    with pytest.raises(WorkerConfigurationError):
        shown_removal_from_environment(SimpleNamespace(), lambda key, default='': enabled)


@pytest.mark.parametrize('resource,backend', [('gpu', 'cuda'), ('gpu', 'metal'), ('cpu', 'metal')])
def test_shown_removal_copy_refuses_non_cpu_worker(resource, backend):
    config=SimpleNamespace(resource_class=resource, backend=backend)
    env={'PHOTO_SHOWN_REMOVAL_ENABLED': 'true'}
    with pytest.raises(WorkerConfigurationError, match='CPU worker'):
        shown_removal_from_environment(config, lambda key, default='': env.get(key, default))


def test_preview_pins_do_not_enable_shown_removal_copy():
    env={'PHOTO_EXPOSURE_PREVIEW_ENABLED': 'true',
         'PHOTO_EXPOSURE_PREVIEW_MODEL_LOCK_SHA': 'a'*64}
    assert shown_removal_from_environment(SimpleNamespace(), lambda key, default='': env.get(key, default)) is None


def test_shown_removal_copy_requires_own_measured_pins():
    config=SimpleNamespace(resource_class='cpu', backend='cpu', icc_root=None)
    env={'PHOTO_SHOWN_REMOVAL_ENABLED': 'true'}
    with pytest.raises(WorkerConfigurationError, match='measured runtime hashes'):
        shown_removal_from_environment(config, lambda key, default='': env.get(key, default))
