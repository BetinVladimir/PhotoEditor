from types import SimpleNamespace
import pytest
from photo_worker.operations.region.calibration import Qualification
from photo_worker.operations.region.installation import NativeRegionColorProvider
from photo_worker.runtime.executor import OperationFailure


def qualification(backend='mps'):
    return Qualification(runtime_lock_sha='a'*64,model_lock_sha='b'*64,icc_lock_sha='c'*64,
        device_profile_sha='d'*64,dataset_sha='e'*64,policy_sha='f'*64,backend=backend)


@pytest.mark.parametrize('backend',['cuda','mps'])
def test_native_region_has_real_exact_backend_and_no_uninstalled_module_claim(tmp_path,backend):
    pin=qualification(backend)
    expected=pin.model_copy(deep=True)
    installed=NativeRegionColorProvider(pin,tmp_path,lambda q:q==expected,1024)
    installed.verify_runtime()
    assert installed.backend==backend
    assert installed.supported_modules==frozenset({'color','strength','restoration','geometry','composition','portrait_detail','background','region_adjustments','series','defects','culling'})
    pin.dataset_sha='0'*64
    assert installed.qualification.dataset_sha=='e'*64
    installed.verify_runtime()
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        installed.admit_resources(SimpleNamespace(check_active=lambda:None,
            lease=SimpleNamespace(execution_profile={'backend':'cpu'})),None)


@pytest.mark.parametrize('result',[None,False,1,'true'])
def test_native_region_never_infers_qualification(tmp_path,result):
    installed=NativeRegionColorProvider(qualification(),tmp_path,lambda q:result,1024)
    with pytest.raises(OperationFailure,match='REGION_RUNTIME_UNAVAILABLE'):
        installed.verify_runtime()


def test_native_region_verifier_cannot_rewrite_frozen_qualification(tmp_path):
    def mutate(q):
        q.dataset_sha='0'*64
        return True
    installed=NativeRegionColorProvider(qualification(),tmp_path,mutate,1024)
    with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
        installed.verify_runtime()


@pytest.mark.parametrize('backend',['cuda','mps'])
def test_masking_catalog_cannot_claim_uninstalled_or_injected_factories(tmp_path,backend):
    # A shape-compatible fake is not an installed qualified ModelPool. Each
    # required scene/face/interactive model must pass real constructor checks.
    fake=SimpleNamespace(lock=SimpleNamespace(lock_id='b'*64),factories={
        'segformer_b0_ade':lambda:None,'bisenet_face19':lambda:None,'mobilesam':lambda:None})
    with pytest.raises(ValueError,match='REGION_MASKING_INSTALLATION_INVALID'):
        NativeRegionColorProvider(qualification(backend),tmp_path,lambda q:True,1024,masking_pool=fake)
