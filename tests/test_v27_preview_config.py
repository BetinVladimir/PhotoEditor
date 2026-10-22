from types import SimpleNamespace
import pytest
from photo_worker.runtime.preview_config import exposure_preview_from_environment
from photo_worker.runtime.service import WorkerConfigurationError


def test_preview_startup_disabled_needs_no_gpu_or_storage():
    assert exposure_preview_from_environment(SimpleNamespace(),lambda key,default=None:default) is None


@pytest.mark.parametrize('backend,resource', [('mps','gpu'),('cuda','gpu'),('cpu','gpu')])
def test_preview_startup_requires_cpu(backend,resource):
    config=SimpleNamespace(backend=backend,resource_class=resource)
    with pytest.raises(WorkerConfigurationError,match='CPU worker'):
        exposure_preview_from_environment(config,lambda key,default=None:'true' if key=='PHOTO_EXPOSURE_PREVIEW_ENABLED' else default)


def test_preview_startup_rejects_ambiguous_enablement():
    with pytest.raises(WorkerConfigurationError):
        exposure_preview_from_environment(SimpleNamespace(),lambda key,default=None:'1')
