from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.semantic.preview_background_cleanup_stage import stage_background_cleanup_preview
from test_v27_background_cleanup_preview import background_fixture
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('accepted', [True, False])
def test_background_cleanup_stage_retains_source_metadata_and_admits_before_write(tmp_path, accepted):
    params, _, _ = background_fixture()
    edit, image, raw = source_fixture(); params['source'] = edit['source']
    encoded = np.full((1, 2, 3), .6, np.float32)
    calls = []
    class Artifacts:
        def stage(self, path, reservation, role, media):
            calls.append(role)
            np.testing.assert_array_equal(np.load(path, allow_pickle=False), encoded)
            return ArtifactRef(artifact_id=uuid4(), sha256=sha256(path.read_bytes()).hexdigest())

        def stage_contract(self, value, role):
            calls.append(role)
            result = value.model_dump(mode='json')
            for key in ('color', 'canvas', 'transform_chain', 'alpha'):
                assert result[key] == image[key]
            return ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(value)).hexdigest())

    artifacts = Artifacts()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        options = dict(verify_admission=lambda _: accepted, check_active=lambda: None,
            max_bytes=128*1024**2, max_output_bytes=2*1024**2)
        if accepted:
            result = stage_background_cleanup_preview(artifacts, params, raw, encoded, **options)
            assert result.bytes_written > encoded.nbytes
            assert calls == ['pixels', 'working_image']
        else:
            with pytest.raises(ValueError, match='ADMISSION_DENIED'):
                stage_background_cleanup_preview(artifacts, params, raw, encoded, **options)
            assert calls == []


@pytest.mark.parametrize('boundary', ['compute', 'source_read'])
@pytest.mark.parametrize('loss', ['cancel', 'admission'])
def test_cleanup_compute_and_stage_stops_before_further_io_on_authority_loss(monkeypatch, boundary, loss):
    from types import SimpleNamespace
    from photo_worker.operations.semantic import preview_background_cleanup_stage as module
    params, _, _ = background_fixture()
    edit, _, source_raw = source_fixture()
    params['source'] = edit['source']
    lost = False
    calls = []
    def compute(*a, **k):
        nonlocal lost
        calls.append('compute')
        if boundary == 'compute':
            lost = True
        return SimpleNamespace(original=np.zeros((1,2,3), np.float32),
            candidate=np.zeros((1,2,3), np.float32))
    class Artifacts:
        def contract_bytes(self, ref, model):
            nonlocal lost
            calls.append('source_read')
            lost = True
            return source_raw
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during cleanup staging')
    def forbidden(*a, **k):
        raise AssertionError('staging reached after authority loss')
    monkeypatch.setattr('photo_worker.operations.semantic.preview_background_cleanup.execute_artifact_background_cleanup_preview', compute)
    monkeypatch.setattr(module, 'stage_background_cleanup_preview', forbidden)
    with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
        module.execute_and_stage_background_cleanup_preview(Artifacts(), params, object(),
            verify_admission=lambda _: not (lost and loss == 'admission'),
            verify_origin=lambda *a: True, verify_color=lambda _: True,
            check_active=active, max_bytes=2**30, max_output_bytes=2**30)
    assert calls == (['compute'] if boundary == 'compute' else ['compute', 'source_read'])


@pytest.mark.parametrize('boundary', ['pixels', 'working_image'])
@pytest.mark.parametrize('loss', ['cancel', 'admission'])
def test_cleanup_stage_never_returns_success_after_upload_authority_loss(tmp_path, boundary, loss):
    params, _, _ = background_fixture()
    edit, _, raw = source_fixture()
    params['source'] = edit['source']
    calls = []
    lost = False
    def reference(body, role):
        nonlocal lost
        calls.append(role)
        if role == boundary:
            lost = True
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    class Artifacts:
        def stage(self, path, reservation, role, media):
            return reference(path.read_bytes(), role)
        def stage_contract(self, value, role):
            return reference(canonical_bytes(value), role)
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during upload')
    artifacts = Artifacts()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
            stage_background_cleanup_preview(artifacts, params, raw, np.full((1,2,3), .6, np.float32),
                verify_admission=lambda _: not (lost and loss == 'admission'), check_active=active,
                max_bytes=128*1024**2, max_output_bytes=2*1024**2)
    assert calls == (['pixels'] if boundary == 'pixels' else ['pixels', 'working_image'])
