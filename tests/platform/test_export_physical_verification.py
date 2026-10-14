import hashlib
from uuid import uuid4
import numpy as np
import pytest
import tifffile
from PIL import Image
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodeParams
from photo_worker.operations.export.encode import _verify


def params(profile, format='jpeg'):
    return EncodeParams(image=ArtifactRef(artifact_id=uuid4(), sha256='a'*64),
        format=format, icc_id='srgb_v1', icc_sha256=hashlib.sha256(profile).hexdigest(),
        quality=90 if format=='jpeg' else None, alpha='matte', matte_rgb=[1., 1., 1.], metadata_policy='strip')


def test_jpeg_exact_icc_and_stripped_metadata(tmp_path):
    profile = b'test ICC binding'
    path = tmp_path / 'image.jpg'
    Image.new('RGB', (4, 3), 'red').save(path, icc_profile=profile)
    _verify(path, params(profile), 4, 3, False)
    with pytest.raises(ValueError, match='ENCODE_ICC_MISMATCH'):
        _verify(path, params(b'other ICC'), 4, 3, False)
    exif = Image.Exif(); exif[315] = 'private source author'
    Image.new('RGB', (4, 3), 'red').save(path, icc_profile=profile, exif=exif)
    with pytest.raises(ValueError, match='ENCODE_METADATA_NOT_STRIPPED'):
        _verify(path, params(profile), 4, 3, False)


def test_tiff_exact_icc_metadata_and_single_page(tmp_path):
    profile = b'test TIFF ICC binding'
    path = tmp_path / 'image.tiff'
    pixels = np.zeros((3, 4, 3), dtype=np.uint16)
    tags = [(34675, 'B', len(profile), profile, False)]
    tifffile.imwrite(path, pixels, photometric='rgb', metadata=None, extratags=tags)
    _verify(path, params(profile, 'tiff'), 4, 3, False)
    with pytest.raises(ValueError, match='ENCODE_ICC_MISMATCH'):
        _verify(path, params(b'other ICC', 'tiff'), 4, 3, False)
    tifffile.imwrite(path, pixels, photometric='rgb', metadata=None, description='source secret', extratags=tags)
    with pytest.raises(ValueError, match='ENCODE_METADATA_NOT_STRIPPED'):
        _verify(path, params(profile, 'tiff'), 4, 3, False)
    with tifffile.TiffWriter(path) as writer:
        writer.write(pixels, photometric='rgb', metadata=None, extratags=tags)
        writer.write(pixels, photometric='rgb', metadata=None, extratags=tags)
    with pytest.raises(ValueError, match='TIFF_VERIFY_FAILED'):
        _verify(path, params(profile, 'tiff'), 4, 3, False)


def test_png_rejects_eight_bit_substitution(tmp_path):
    profile = b'test PNG ICC binding'
    path = tmp_path / 'eight-bit.png'
    Image.new('RGB', (4, 3), 'red').save(path, icc_profile=profile)
    with pytest.raises(ValueError, match='PNG_LAYOUT_MISMATCH'):
        _verify(path, params(profile, 'png'), 4, 3, False)


def test_png_writer_preserves_sixteen_bit_layout(tmp_path):
    from photo_worker.operations.export.encode import _write
    from PIL import ImageCms
    profile = ImageCms.ImageCmsProfile(ImageCms.createProfile('sRGB')).tobytes()
    path = tmp_path / 'sixteen-bit.png'
    pixels = np.full((3, 4, 3), .1234567, dtype=np.float32)
    p = params(profile, 'png')
    _write(path, pixels, profile, p)
    _verify(path, p, 4, 3, False)
    with pytest.raises(ValueError, match='PNG_LAYOUT_MISMATCH'):
        _verify(path, p, 5, 3, False)


def test_tiff_rejects_associated_alpha(tmp_path):
    profile = b'test TIFF ICC binding'
    path = tmp_path / 'associated.tiff'
    pixels = np.zeros((3, 4, 4), dtype=np.uint16)
    tifffile.imwrite(path, pixels, photometric='rgb', extrasamples='ASSOCALPHA', metadata=None,
        extratags=[(34675, 'B', len(profile), profile, False)])
    with pytest.raises(ValueError, match='TIFF_ALPHA_MISMATCH'):
        _verify(path, params(profile, 'tiff'), 4, 3, True)
