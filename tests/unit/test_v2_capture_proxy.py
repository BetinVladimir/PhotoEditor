from uuid import UUID

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.capture.adapter import validate_proxy
from photo_worker.operations.session.contracts import VersionBinding


def test_proxy_cannot_switch_source_canvas_or_color_lineage():
    ref = ArtifactRef(artifact_id=UUID(int=1), sha256='a' * 64)
    image = WorkingImage(pixels=ref, canvas=Canvas(width=64, height=64, orientation=1,
        space_id='work', source_sha256='b' * 64), color=ColorSpace(
        profile_id='work_rec2020_gamma24_v1', icc_sha256='c' * 64, encoding='gamma24'),
        dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
    binding = VersionBinding(asset_version_id=UUID(int=2), source=ref, canvas_hash=canonical_hash(image.canvas))
    validate_proxy(image, image, binding)
    wrong = image.model_copy(update={'canvas': image.canvas.model_copy(update={'source_sha256': 'd' * 64})})
    with pytest.raises(ValueError, match='BINDING_INVALID'):
        validate_proxy(image, wrong, binding)
    wrong = image.model_copy(update={'canvas': image.canvas.model_copy(update={'width': 32})})
    with pytest.raises(ValueError, match='TRANSFORM_INVALID'):
        validate_proxy(image, wrong, binding)


def test_capture_preparation_preserves_values_source_and_transform():
    import numpy as np
    from photo_worker.operations.capture.prepare import prepare_proxy_pixels, bind_proxy
    ref = ArtifactRef(artifact_id=UUID(int=1), sha256='a' * 64)
    source = WorkingImage(pixels=ref, canvas=Canvas(width=4096, height=4, orientation=1,
        space_id='work', source_sha256='b' * 64), color=ColorSpace(
        profile_id='work_rec2020_gamma24_v1', icc_sha256='c' * 64, encoding='gamma24'),
        dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
    rgb = np.full((4, 4096, 3), 1.5, dtype=np.float32)
    rgb[:, :2048] = -.25
    original = rgb.copy()
    prepared = prepare_proxy_pixels(source, rgb)
    assert prepared.rgb.shape == (2, 2048, 3)
    assert prepared.rgb.min() == -.25 and prepared.rgb.max() == 1.5
    np.testing.assert_array_equal(rgb, original)
    proxy = bind_proxy(source, prepared, ref, None)
    binding = VersionBinding(asset_version_id=UUID(int=2), source=ref,
                             canvas_hash=canonical_hash(source.canvas))
    validate_proxy(source, proxy, binding)
    assert proxy.transform_chain.steps[-1].forward_matrix == [.5, 0, -.25, 0, .5, -.25, 0, 0, 1]
    rgb[0, 0, 0] = np.nan
    with pytest.raises(ValueError, match='NONFINITE'):
        prepare_proxy_pixels(source, rgb)


def test_capture_preparation_checks_cancellation_and_alpha_binding():
    import numpy as np
    from photo_worker.operations.capture.prepare import prepare_proxy_pixels
    ref = ArtifactRef(artifact_id=UUID(int=1), sha256='a' * 64)
    source = WorkingImage(pixels=ref, canvas=Canvas(width=2, height=2, orientation=1,
        space_id='work', source_sha256='b' * 64), color=ColorSpace(
        profile_id='work_rec2020_gamma24_v1', icc_sha256='c' * 64, encoding='gamma24'),
        dtype='float32', alpha=ref, transform_chain=TransformChain.create([]))
    rgb = np.zeros((2, 2, 3), dtype=np.float32)
    with pytest.raises(ValueError, match='SOURCE_INVALID'):
        prepare_proxy_pixels(source, rgb)
    def cancelled():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        prepare_proxy_pixels(source, rgb, np.zeros((2, 2), dtype=np.uint16), cancelled)


def test_capture_proxy_publishes_children_before_manifest(tmp_path):
    from contextlib import contextmanager
    import numpy as np
    from photo_worker.operations.capture.prepare import prepare_proxy_pixels, publish_proxy
    ref = ArtifactRef(artifact_id=UUID(int=1), sha256='a' * 64)
    source = WorkingImage(pixels=ref, canvas=Canvas(width=3, height=2, orientation=1,
        space_id='work', source_sha256='b' * 64), color=ColorSpace(
        profile_id='work_rec2020_gamma24_v1', icc_sha256='c' * 64, encoding='gamma24'),
        dtype='float32', alpha=ref, transform_chain=TransformChain.create([]))
    rgb = np.full((2, 3, 3), 1.25, dtype=np.float32)
    alpha = np.array([[0, 65535, 32768], [100, 200, 300]], dtype=np.uint16)
    prepared = prepare_proxy_pixels(source, rgb, alpha)
    class Reservation:
        def path(self, name, suffix):
            return tmp_path / (name + suffix)
    class Scratch:
        @contextmanager
        def reserve(self, amount):
            assert amount >= rgb.nbytes + alpha.nbytes
            yield Reservation()
    class Artifacts:
        scratch = Scratch()
        roles = []
        arrays = []
        def stage(self, path, reservation, role, media):
            assert media == 'application/octet-stream'
            self.roles.append(role)
            self.arrays.append(np.load(path, allow_pickle=False))
            return ArtifactRef(artifact_id=UUID(int=len(self.roles) + 10), sha256='d' * 64)
        def stage_contract(self, image, role):
            assert self.roles == ['working_rgb', 'working_alpha']
            assert image.pixels.artifact_id == UUID(int=11)
            assert image.alpha.artifact_id == UUID(int=12)
            self.roles.append(role)
            return ArtifactRef(artifact_id=UUID(int=13), sha256=canonical_hash(image))
    artifacts = Artifacts()
    published = publish_proxy(artifacts, source, prepared, lambda: None)
    assert artifacts.roles == ['working_rgb', 'working_alpha', 'working_image']
    np.testing.assert_array_equal(artifacts.arrays[0], rgb)
    np.testing.assert_array_equal(artifacts.arrays[1], alpha)
    assert len(published.outputs) == 3 and published.bytes_written > rgb.nbytes + alpha.nbytes


def test_capture_model_preview_does_not_destroy_clipping_evidence():
    import numpy as np
    from photo_worker.operations.capture.prepare import capture_model_preview
    rgb = np.array([[[-.2, .5, 1.5], [1., 1., 1.]]], dtype=np.float32)
    original = rgb.copy()
    alpha = np.array([[65535, 0]], dtype=np.uint16)
    srgb = capture_model_preview(rgb, alpha)
    assert srgb.dtype == np.float32 and np.isfinite(srgb).all()
    assert srgb.min() >= 0 and srgb.max() <= 1.000001
    assert not srgb[0, 1].any()
    np.testing.assert_array_equal(rgb, original)


def test_capture_evidence_accepts_out_of_range_proxy_for_models():
    from types import SimpleNamespace as NS
    import numpy as np
    from photo_worker.operations.capture.evidence import infer_face_evidence
    entry = NS(id='eye_open', approved_for_install=True, backends={'cpu': NS(status='passed')})
    pool = NS(lock=NS(models=[entry]))
    work = np.full((2, 2, 3), 1.2, dtype=np.float32)
    result = infer_face_evidence(NS(check_active=lambda: None), work, NS(faces=[]),
                                NS(), pool, 'cpu', NS(checks=['eyes']))
    assert result == {} and work.max() == np.float32(1.2)


def test_capture_evidence_forwards_alpha_to_model_preview(monkeypatch):
    from types import SimpleNamespace as NS
    import numpy as np
    from photo_worker.operations.capture import evidence
    work = np.zeros((2, 2, 3), dtype=np.float32)
    alpha = np.zeros((2, 2), dtype=np.uint16)
    seen = []
    def preview(rgb, opacity):
        assert rgb is work and opacity is alpha
        seen.append(True)
        return rgb
    monkeypatch.setattr(evidence, 'capture_model_preview', preview)
    entry = NS(id='glare_mask', approved_for_install=True, backends={'cpu': NS(status='passed')})
    result = evidence.infer_face_evidence(NS(check_active=lambda: None), work, NS(faces=[]),
        NS(), NS(lock=NS(models=[entry])), 'cpu', NS(checks=['glare']), alpha=alpha)
    assert result == {} and seen == [True]


def test_capture_face_preparation_rejects_unqualified_models_before_inference():
    from types import SimpleNamespace as NS
    import numpy as np
    from photo_worker.models.install import ModelError
    from photo_worker.operations.capture.prepare import prepare_proxy_pixels, prepare_face_observations
    ref = ArtifactRef(artifact_id=UUID(int=1), sha256='a' * 64)
    source = WorkingImage(pixels=ref, canvas=Canvas(width=2, height=2, orientation=1,
        space_id='work', source_sha256='b' * 64), color=ColorSpace(
        profile_id='work_rec2020_gamma24_v1', icc_sha256='c' * 64, encoding='gamma24'),
        dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
    binding = VersionBinding(asset_version_id=UUID(int=2), source=ref, canvas_hash=canonical_hash(source.canvas))
    prepared = prepare_proxy_pixels(source, np.zeros((2, 2, 3), dtype=np.float32))
    calibration = NS(qualified=True, model_lock_sha='d' * 64)
    pool = NS(lock=NS(lock_id='d' * 64, models=[]))
    with pytest.raises(ModelError, match='MODEL_UNAVAILABLE'):
        prepare_face_observations(source, prepared, binding, calibration, pool, 'cpu', lambda: None)


def test_capture_proxy_float_alpha_survives_preparation_and_preview():
    import numpy as np
    from photo_worker.operations.capture.prepare import prepare_proxy_pixels,bind_proxy,capture_model_preview
    ref=ArtifactRef(artifact_id=UUID(int=1),sha256='a'*64)
    source=WorkingImage(pixels=ref,alpha=ref,dtype='float32',
        canvas=Canvas(width=4,height=2,orientation=1,space_id='work',source_sha256='b'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    rgb=np.full((2,4,3),.5,np.float32);alpha=np.full((2,4),.375,np.float32);alpha[0,0]=0
    prepared=prepare_proxy_pixels(source,rgb,alpha)
    assert prepared.alpha.dtype==np.float32
    np.testing.assert_array_equal(prepared.alpha,alpha)
    bind_proxy(source,prepared,ref,ref)
    preview=capture_model_preview(prepared.rgb,prepared.alpha)
    np.testing.assert_array_equal(preview[0,0],0)
    assert np.all(preview[1,1]>0)
    invalid=alpha.copy();invalid[0,0]=np.nan
    with pytest.raises(ValueError,match='ALPHA_INVALID'):prepare_proxy_pixels(source,rgb,invalid)
