import hashlib
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
from PIL import Image, JpegImagePlugin
import pytest

from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodeParams, ExportSize
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.export.encode import execute
from photo_worker.runtime.scratch import ScratchSpace


def test_export_encode_color_managed_jpeg(tmp_path):
    registry = ICCRegistry(Path(__file__).resolve().parents[2] / 'resources/icc')
    work = registry.resolve('work_rec2020_gamma24_v1')
    srgb = registry.resolve('srgb_v1')
    image_ref, pixel_ref = (ArtifactRef(artifact_id=uuid4(), sha256=char * 64) for char in ('a', 'b'))
    image = WorkingImage(pixels=pixel_ref, canvas=Canvas(width=8, height=4, orientation=1,
        space_id='oriented:' + 'c' * 64, source_sha256='c' * 64),
        color=ColorSpace(profile_id=work.id, icc_sha256=work.sha256, encoding='gamma24'),
        dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
    params = EncodeParams(image=image_ref, format='jpeg', icc_id='srgb_v1', icc_sha256=srgb.sha256,
        size=ExportSize(long_edge=4), quality=95, subsampling='444', alpha='matte',
        matte_rgb=[1.0, 1.0, 1.0], metadata_policy='strip')
    pixel_path = tmp_path / 'pixels.npy'
    np.save(pixel_path, np.full((4, 8, 3), 0.5, dtype='<f4'), allow_pickle=False)

    class Context:
        lease = SimpleNamespace(execution_profile={'backend': 'cpu'}, command=SimpleNamespace(inputs=[image_ref, pixel_ref]))

        def check_active(self): pass
        def progress(self, done, total): assert 0 <= done <= total

    class Artifacts:
        def __init__(self):
            self.scratch = ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 8 * 1024**2, min_free_bytes=0)
            self.encoded = None
            self.manifest = None

        def params(self, model): return params
        def contract(self, ref, model): return image

        @contextmanager
        def input(self, ref, suffix='.bin'):
            yield SimpleNamespace(path=pixel_path)

        def stage(self, path, reservation, role, media_type):
            assert role == 'encoded_image' and media_type == 'image/jpeg'
            self.encoded = path.read_bytes()
            return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(self.encoded).hexdigest())

        def stage_contract(self, value, role):
            assert role == 'encoded_manifest'
            self.manifest = value
            return ArtifactRef(artifact_id=uuid4(), sha256='d' * 64)

    artifacts = Artifacts()
    try:
        result = execute(Context(), artifacts, registry)
        assert len(result.outputs) == 2 and artifacts.manifest.width == 4 and artifacts.manifest.height == 2
        output = tmp_path / 'result.jpg'
        output.write_bytes(artifacts.encoded)
        with Image.open(output) as encoded:
            assert encoded.size == (4, 2) and encoded.mode == 'RGB'
            assert hashlib.sha256(encoded.info['icc_profile']).hexdigest() == srgb.sha256
            assert JpegImagePlugin.get_sampling(encoded) == 0
    finally:
        artifacts.scratch.cleanup()


def test_encode_requires_metadata_write_for_non_strip_policy():
    with pytest.raises(ValueError):
        EncodeParams(image=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64), format='jpeg',
            icc_id='srgb_v1', icc_sha256='b' * 64, quality=95, alpha='matte',
            matte_rgb=[1.0, 1.0, 1.0], metadata_policy='safe_camera')


@pytest.mark.parametrize('rgb_value',[-.25,.5,1.5])
@pytest.mark.parametrize('alpha_dtype',['uint16','float32'])
def test_export_encode_reads_uint16_alpha_and_preserves_png(tmp_path,alpha_dtype,rgb_value):
    registry = ICCRegistry(Path(__file__).resolve().parents[2] / 'resources/icc')
    work = registry.resolve('work_rec2020_gamma24_v1')
    srgb = registry.resolve('srgb_v1')
    image_ref = ArtifactRef(artifact_id=uuid4(), sha256='1' * 64)
    pixel_ref = ArtifactRef(artifact_id=uuid4(), sha256='2' * 64)
    alpha_ref = ArtifactRef(artifact_id=uuid4(), sha256='3' * 64)
    image = WorkingImage(pixels=pixel_ref, canvas=Canvas(width=4, height=2, orientation=1,
        space_id='oriented:' + '4' * 64, source_sha256='4' * 64),
        color=ColorSpace(profile_id=work.id, icc_sha256=work.sha256, encoding='gamma24'),
        dtype='float32', alpha=alpha_ref, transform_chain=TransformChain.create([]))
    params = EncodeParams(image=image_ref, format='png', icc_id='srgb_v1', icc_sha256=srgb.sha256,
        subsampling='444', alpha='preserve', metadata_policy='strip')
    pixel_path, alpha_path = tmp_path / 'rgb.npy', tmp_path / 'alpha.npy'
    np.save(pixel_path, np.full((2, 4, 3), rgb_value, dtype='<f4'), allow_pickle=False)
    alpha = np.array([[0, 65535, 0, 65535], [65535, 0, 65535, 0]], dtype='<u2')
    if alpha_dtype=='float32':alpha=alpha.astype(np.float32)/np.float32(65535)
    np.save(alpha_path, alpha, allow_pickle=False)

    class Context:
        lease = SimpleNamespace(execution_profile={'backend': 'cpu'}, command=SimpleNamespace(inputs=[image_ref, pixel_ref, alpha_ref]))
        def check_active(self): pass
        def progress(self, done, total): assert 0 <= done <= total

    class Artifacts:
        def __init__(self):
            self.scratch = ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 8 * 1024**2, min_free_bytes=0)
            self.encoded = None
            self.manifest = None
        def params(self, model): return params
        def contract(self, ref, model): return image
        @contextmanager
        def input(self, ref, suffix='.bin'):
            yield SimpleNamespace(path=pixel_path if ref == pixel_ref else alpha_path)
        def stage(self, path, reservation, role, media_type):
            self.encoded = path.read_bytes()
            return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(self.encoded).hexdigest())
        def stage_contract(self, value, role):
            self.manifest = value
            return ArtifactRef(artifact_id=uuid4(), sha256='5' * 64)

    artifacts = Artifacts()
    try:
        execute(Context(), artifacts, registry)
        output = tmp_path / 'result.png'
        output.write_bytes(artifacts.encoded)
        with Image.open(output) as encoded:
            assert encoded.mode == 'RGBA' and encoded.size == (4, 2)
            values = np.asarray(encoded)
            assert values[0, 0, 3] == 0 and values[0, 1, 3] > 0
            if rgb_value<0:assert np.max(values[0,1,:3])<=2
            if rgb_value>1:assert np.min(values[0,1,:3])>=253
        assert artifacts.manifest.alpha is True
    finally:
        artifacts.scratch.cleanup()
