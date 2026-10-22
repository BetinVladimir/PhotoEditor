from types import SimpleNamespace
import pytest
from photo_worker.models.backend_support import MODEL_BACKENDS, model_backend_supported
from photo_worker.models.lock import MODEL_IDS
from photo_worker.runtime.service import _model_backend_enabled


def test_native_matrix_covers_every_lock_model_and_is_immutable():
    assert set(MODEL_BACKENDS)==set(MODEL_IDS)
    with pytest.raises(TypeError):MODEL_BACKENDS['identity512']=frozenset({'mps'})


@pytest.mark.parametrize('model',['identity512','eye_open','glare_mask','reflection_rgb'])
def test_onnx_models_never_gain_metal_from_forged_passed_lock(model):
    entry=SimpleNamespace(id=model,backends={'mps':SimpleNamespace(status='passed')})
    pool=SimpleNamespace(lock=SimpleNamespace(models=[entry]))
    assert not model_backend_supported(model,'mps')
    assert not _model_backend_enabled(pool,model,'mps')
    assert model_backend_supported(model,'cpu') and model_backend_supported(model,'cuda')


@pytest.mark.parametrize('model',['mobilesam','sd15_inpaint','big_lama','depth_anything_v2_small'])
def test_native_metal_support_does_not_fabricate_qualification(model):
    assert model_backend_supported(model,'mps')
    pool=SimpleNamespace(lock=SimpleNamespace(models=[SimpleNamespace(id=model,backends={'mps':SimpleNamespace(status='unsupported')})]))
    assert not _model_backend_enabled(pool,model,'mps')


@pytest.mark.parametrize('model,backend',[('unknown','cuda'),('identity512','metal'),('mobilesam','unknown')])
def test_unknown_native_combinations_are_unavailable(model,backend):
    assert not model_backend_supported(model,backend)


@pytest.mark.parametrize('model',['identity512','eye_open','glare_mask','reflection_rgb'])
def test_pool_refuses_native_mismatch_before_weights_or_inference_lock(model):
    from photo_worker.models.pool import ModelPool
    from photo_worker.models.install import ModelError
    pool=object.__new__(ModelPool)
    pool.lock=SimpleNamespace(models=[SimpleNamespace(id=model,backends={'mps':SimpleNamespace(status='passed')})])
    pool.factories={model:lambda *args:pytest.fail('unsupported provider instantiated')}
    # No inference mutex or weight root exists: refusal must happen first.
    with pytest.raises(ModelError,match='BACKEND_UNAVAILABLE'):
        with pool.acquire(model,'mps',allow_unqualified=True):pytest.fail('unsupported model borrowed')


def test_capture_does_not_attempt_unsupported_metal_eye_or_glare_inference(monkeypatch):
    from photo_worker.operations.capture.evidence import infer_face_evidence
    pool=SimpleNamespace(lock=SimpleNamespace(models=[SimpleNamespace(id=model,approved_for_install=True,backends={'mps':SimpleNamespace(status='passed')}) for model in ['eye_open','glare_mask']]))
    monkeypatch.setattr('photo_worker.operations.capture.evidence.capture_model_preview',lambda *args:pytest.fail('unsupported Metal inference materialized pixels'))
    ctx=SimpleNamespace(check_active=lambda:None)
    assert infer_face_evidence(ctx,None,None,None,pool,'mps',SimpleNamespace(checks=['eyes','glare']))=={}


@pytest.mark.parametrize('backend,expected',[('cpu',True),('cuda',False),('mps',False)])
def test_u2net_matrix_matches_cpu_only_native_adapter(backend,expected):
    assert model_backend_supported('u2net',backend) is expected
