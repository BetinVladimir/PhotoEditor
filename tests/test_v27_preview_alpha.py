import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput
from photo_worker.operations.semantic.preview_loader import load_exposure_preview_alpha
from test_v27_exposure_preview_binding import fixture
from test_v27_exposure_source import source_fixture


class NoIO:
    def input(self,*args,**kwargs):raise AssertionError('unexpected alpha IO')


@pytest.mark.parametrize('accepted',[False,None,1])
def test_preview_alpha_requires_explicit_admission_before_io(accepted):
    params,_,_=fixture();edit,image,raw=source_fixture();params['source']=edit['source']
    owned=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    with pytest.raises(ValueError,match='ADMISSION_DENIED'):
        load_exposure_preview_alpha(NoIO(),owned,raw,verify_admission=lambda value:accepted,
            max_bytes=32*1024**2,check_active=lambda:None)


def test_preview_without_alpha_preserves_opaque_source_without_io():
    params,_,_=fixture();edit,image,raw=source_fixture();params['source']=edit['source']
    assert image.get('alpha') is None
    owned=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    assert load_exposure_preview_alpha(NoIO(),owned,raw,verify_admission=lambda value:True,
        max_bytes=32*1024**2,check_active=lambda:None) is None


@pytest.mark.parametrize('loss', ['cancel', 'admission'])
@pytest.mark.parametrize('operation', ['local_exposure', 'background_cleanup'])
def test_preview_alpha_denies_parser_after_authority_loss_during_open(tmp_path, monkeypatch, loss, operation):
    from contextlib import contextmanager
    from hashlib import sha256
    from types import SimpleNamespace
    from uuid import uuid4
    from photo_worker.operations.semantic import preview_loader
    from test_v27_background_cleanup_preview import background_fixture
    params, _, _ = background_fixture() if operation == 'background_cleanup' else fixture()
    edit, image, _ = source_fixture()
    image['alpha'] = dict(artifact_id=str(uuid4()), sha256='f'*64)
    raw = canonical_bytes(image)
    edit['source']['source']['sha256'] = sha256(raw).hexdigest()
    params['source'] = edit['source']
    lost = False
    events = []
    class Artifacts:
        @contextmanager
        def input(self, ref, *, suffix):
            nonlocal lost
            events.append('open')
            lost = True
            try:
                yield SimpleNamespace(path=tmp_path/'must-not-read.npy')
            finally:
                events.append('close')
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during alpha open')
    def forbidden(*a, **k):
        raise AssertionError('alpha parser reached after authority loss')
    monkeypatch.setattr('photo_worker.runtime.arrays.read_alpha_array', forbidden)
    load = (preview_loader.load_background_cleanup_preview_alpha if operation == 'background_cleanup'
        else preview_loader.load_exposure_preview_alpha)
    with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
        load(Artifacts(), params, raw,
            verify_admission=lambda _: not (lost and loss == 'admission'),
            max_bytes=32*1024**2, check_active=active)
    assert events == ['open', 'close']
