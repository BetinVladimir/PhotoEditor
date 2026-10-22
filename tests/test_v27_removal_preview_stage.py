from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.semantic.preview_removal_stage import stage_removal_preview
from test_v27_removal_preview_binding import removal_fixture
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('accepted', [True, False])
def test_removal_stage_retains_source_metadata_and_admits_before_write(tmp_path, accepted):
    params, _, _ = removal_fixture()
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
            result = stage_removal_preview(artifacts, params, raw, encoded, **options)
            assert result.bytes_written > encoded.nbytes
            assert calls == ['pixels', 'working_image']
        else:
            with pytest.raises(ValueError, match='ADMISSION_DENIED'):
                stage_removal_preview(artifacts, params, raw, encoded, **options)
            assert calls == []


@pytest.mark.parametrize('failure', [None, 'pixel_sha', 'image_sha',
    'pixel_alpha_alias', 'image_source_alias', 'pixel_upload', 'image_upload',
    'pixel_revoke', 'image_revoke', 'memory_budget', 'output_budget'])
def test_removal_pixel_publication_failures_preserve_source_and_fail_closed(tmp_path, failure):
    from photo_worker.contracts.base import canonical_hash
    params, _, _ = removal_fixture()
    edit, image, _ = source_fixture()
    image['alpha'] = dict(artifact_id=str(uuid4()), sha256='f'*64)
    raw = canonical_bytes(image)
    edit['source']['source']['sha256'] = sha256(raw).hexdigest()
    edit['source']['canvas_hash'] = canonical_hash(image['canvas'])
    params['source'] = edit['source']
    calls = []
    allowed = True
    encoded = np.full((1, 2, 3), .6, np.float32)

    class Artifacts:
        def stage(self, path, reservation, role, media):
            nonlocal allowed
            calls.append(role)
            assert media == 'application/x-npy'
            np.testing.assert_array_equal(np.load(path, allow_pickle=False), encoded)
            if failure == 'pixel_upload':
                raise RuntimeError('pixel quota exhausted')
            if failure == 'pixel_revoke':
                allowed = False
            return ArtifactRef(
                artifact_id=image['alpha']['artifact_id'] if failure == 'pixel_alpha_alias' else uuid4(),
                sha256='0'*64 if failure == 'pixel_sha' else sha256(path.read_bytes()).hexdigest())

        def stage_contract(self, value, role):
            nonlocal allowed
            calls.append(role)
            result = value.model_dump(mode='json')
            for key in ('color', 'canvas', 'transform_chain', 'alpha', 'dtype'):
                assert result[key] == image[key]
            if failure == 'image_upload':
                raise RuntimeError('image quota exhausted')
            if failure == 'image_revoke':
                allowed = False
            return ArtifactRef(
                artifact_id=params['source']['source']['artifact_id'] if failure == 'image_source_alias' else uuid4(),
                sha256='0'*64 if failure == 'image_sha' else sha256(canonical_bytes(value)).hexdigest())

    artifacts = Artifacts()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        options = dict(verify_admission=lambda _: allowed, check_active=lambda: None,
            max_bytes=1 if failure == 'memory_budget' else 128*1024**2,
            max_output_bytes=1 if failure == 'output_budget' else 2*1024**2)
        if failure is None:
            result = stage_removal_preview(artifacts, params, raw, encoded, **options)
            assert result.bytes_written > encoded.nbytes
            assert calls == ['pixels', 'working_image']
        else:
            with pytest.raises((ValueError, RuntimeError)):
                stage_removal_preview(artifacts, params, raw, encoded, **options)
            if failure in ('memory_budget', 'output_budget'):
                assert calls == []
            if failure in ('pixel_upload', 'pixel_sha', 'pixel_revoke'):
                assert calls == ['pixels']
    assert raw == canonical_bytes(image)
    np.testing.assert_array_equal(encoded, np.full((1, 2, 3), .6, np.float32))
