"""No GPU/model IO: installation selection must precede unsupported providers."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime.service import _select_cuda_only_backend, WorkerConfigurationError
from photo_worker.runtime.reflection_config import reflection_installation_from_environment
from photo_worker.runtime.glare_config import glare_donor_installation_from_environment
from photo_worker.runtime.learning_config import learning_evaluation_from_environment


def test_metal_preserves_region_and_omits_still_cuda_only_providers():
    providers=tuple(object() for _ in range(4))
    assert _select_cuda_only_backend('mps',*providers)==(providers[0],None,None,None)

def test_metal_keeps_installed_mps_region_but_omits_declared_cuda_region():
    compatible=SimpleNamespace(backend='mps')
    incompatible=SimpleNamespace(backend='cuda')
    assert _select_cuda_only_backend('mps',compatible,None,None,None)[0] is compatible
    assert _select_cuda_only_backend('mps',incompatible,None,None,None)[0] is None


@pytest.mark.parametrize('backend',['cuda','cpu'])
def test_other_backends_preserve_provider_identity_for_strict_validation(backend):
    providers=tuple(object() for _ in range(4))
    assert all(a is b for a,b in zip(_select_cuda_only_backend(backend,*providers),providers))


@pytest.mark.parametrize('reader',[reflection_installation_from_environment,glare_donor_installation_from_environment])
def test_metal_omits_unsupported_environment_installations_before_pin_reads(reader):
    config=SimpleNamespace(resource_class='gpu',backend='mps')
    def env(name,default=''):
        if name in ('PHOTO_REFLECTION_ENABLED','PHOTO_GLARE_DONOR_ENABLED'):return 'true'
        pytest.fail('Unsupported installation read model/runtime/object-store pins')
    assert reader(config,env) is None


def test_metal_omits_cuda_learning_before_qualification_file_reads():
    config=SimpleNamespace(resource_class='gpu',backend='mps')
    assert learning_evaluation_from_environment(config,lambda *args:pytest.fail('CUDA qualification read on Metal')) is None


@pytest.mark.parametrize('reader,flag',[(reflection_installation_from_environment,'PHOTO_REFLECTION_ENABLED'),(glare_donor_installation_from_environment,'PHOTO_GLARE_DONOR_ENABLED')])
def test_metal_still_rejects_malformed_enablement(reader,flag):
    config=SimpleNamespace(resource_class='gpu',backend='mps')
    with pytest.raises(WorkerConfigurationError,match='enablement'):
        reader(config,lambda name,default='':'invalid' if name==flag else default)


@pytest.mark.parametrize('reader,flag',[(reflection_installation_from_environment,'PHOTO_REFLECTION_ENABLED'),(glare_donor_installation_from_environment,'PHOTO_GLARE_DONOR_ENABLED')])
def test_supported_cpu_configuration_is_still_required(reader,flag):
    config=SimpleNamespace(resource_class='gpu',backend='cpu',model_root=None,model_lock=None)
    with pytest.raises(WorkerConfigurationError):
        reader(config,lambda name,default='':'true' if name==flag else default)


@pytest.mark.parametrize('status,expected',[('unsupported',False),('untested',True),('passed',True)])
def test_model_backend_selection_only_omits_explicit_unsupported_metal(status,expected):
    from photo_worker.runtime.service import _model_backend_enabled
    entry=SimpleNamespace(id='mobilesam',backends={'mps':SimpleNamespace(status=status)})
    pool=SimpleNamespace(lock=SimpleNamespace(models=[entry]))
    assert _model_backend_enabled(pool,'mobilesam','mps') is expected
    # Untested remains subject to the normal qualification barrier; it is not
    # converted into executable qualification or substituted with another backend.
    assert _model_backend_enabled(pool,'mobilesam','cuda') is True
