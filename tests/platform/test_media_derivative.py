from pathlib import Path
import ssl
import numpy as np
from PIL import Image
from uuid import uuid4

import pytest
from pydantic import ValidationError

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.media import MediaParams
from photo_worker.color.icc import ICCRegistry
from photo_worker.operations.media.derivative import _read_master, _write, dimensions, media_operations, media_profile_hash
from photo_worker.runtime.artifacts import ArtifactConfig


def ref():
    return ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)


def test_derivative_dimensions_never_upscale_and_preserve_aspect():
    assert dimensions(6000, 4000, 'thumb320') == (320, 213)
    assert dimensions(4000, 6000, 'preview1600') == (1067, 1600)
    assert dimensions(100, 50, 'preview2560') == (100, 50)


def test_media_params_are_strict_and_variant_bounded():
    value = MediaParams(image=ref(), variant='thumb320', profile_hash='b' * 64)
    assert value.variant == 'thumb320'
    with pytest.raises(ValidationError):
        MediaParams(image=ref(), variant='thumb321', profile_hash='b' * 64)
    with pytest.raises(ValidationError):
        MediaParams(image=ref(), variant='thumb320', profile_hash='b' * 64, unknown=True)
    master = MediaParams(image=ref(), variant='thumb320', profile_hash='b' * 64, source_kind='master', suffix='.jpg')
    assert master.source_kind == 'master'
    with pytest.raises(ValidationError):
        MediaParams(image=ref(), variant='thumb320', profile_hash='b' * 64, source_kind='master')
    with pytest.raises(ValidationError):
        MediaParams(image=ref(), variant='thumb320', profile_hash='b' * 64, suffix='.jpg')


def test_media_registry_installs_both_operations(tmp_path):
    root = Path(__file__).resolve().parents[2]
    config = ArtifactConfig(tmp_path, 'https://gateway.invalid', ssl.create_default_context(), 1, 1, 1)
    operations = media_operations(config, root / 'resources/icc')
    assert [(op.name, op.resource_class) for op in operations] == [
        ('media.thumbnail', 'media'), ('media.preview', 'media')
    ]


def test_derivative_writer_verifies_jpeg_and_alpha_webp(tmp_path):
    root = Path(__file__).resolve().parents[2]
    profile = ICCRegistry(root / 'resources/icc').resolve('srgb_v1').data
    rgb = np.linspace(0, 1, 12 * 8 * 3, dtype=np.float32).reshape(8, 12, 3)
    alpha = np.linspace(0, 1, 12 * 8, dtype=np.float32).reshape(8, 12)
    jpeg, webp = tmp_path / 'image.jpg', tmp_path / 'image.webp'
    _write(jpeg, rgb, None, profile, 82)
    _write(webp, rgb, alpha, profile, 88)
    with Image.open(jpeg) as image:
        assert image.size == (12, 8) and image.mode == 'RGB' and image.info.get('icc_profile')
    with Image.open(webp) as image:
        assert image.size == (12, 8) and image.mode == 'RGBA' and image.info.get('icc_profile')
    admitted=[]
    rgb16, decoded_alpha, embedded = _read_master(webp, lambda: None,admit=lambda w,h,a:admitted.append((w,h,a)))
    assert admitted==[(12,8,True)]
    assert rgb16.shape == (8, 12, 3) and decoded_alpha.shape == (8, 12)
    assert len(embedded) == len(profile)


def test_media_output_policies_are_bounded():
    root = Path(__file__).resolve().parents[2]
    sql = (root / 'backend/db/migrations/00096_media_output_policies.sql').read_text()
    assert "('media.thumbnail','1','derivative'" in sql
    assert "('media.preview','1','derivative'" in sql
    assert sql.count("'derivative_manifest'") == 2


def test_media_profile_hash_binds_every_rendering_choice():
    sha = 'a' * 64
    assert media_profile_hash('thumb320', sha) == 'ebe90336177eb9b7dd29dd048d5a4ede9fcfaa9e87ede89e1391c7ea57f259f4'
    assert media_profile_hash('thumb320', sha) != media_profile_hash('thumb640', sha)


def test_master_ram_rejection_precedes_pixel_decode(tmp_path,monkeypatch):
    import pyvips
    from photo_worker.runtime.executor import OperationFailure
    class Header:
        width=10000;height=10000;bands=4;format='uchar'
        decoded=False
        def autorot(self):return self
        def get_typeof(self,name):return 1
        def get(self,name):return bytes(128)
        def write_to_memory(self):self.decoded=True;raise AssertionError('pixel decode must not start')
    header=Header()
    monkeypatch.setattr(pyvips.Image,'new_from_file',lambda *args,**kwargs:header)
    def refuse(*args):raise OperationFailure('RAM_ADMISSION_EXCEEDED')
    with pytest.raises(OperationFailure):_read_master(tmp_path/'master.png',lambda:None,admit=refuse)
    assert not header.decoded
