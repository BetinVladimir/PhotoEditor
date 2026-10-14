"""V28 glare staging refuses absent authority before physical material IO."""

from uuid import uuid4
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.portrait import glare_stage


def ref():
    return {'artifact_id': uuid4(), 'sha256': 'a' * 64}


def params():
    return PortraitGlareParams(schema_version=1,
        base={'asset_version_id': uuid4(), 'source': ref(), 'canvas_hash': 'b' * 64},
        strength=.5,
        donor={'asset_version_id': uuid4(), 'source': ref(), 'canvas_hash': 'c' * 64},
        glasses_mask=ref(), protected=ref(), mode='donor',
        confirmation=ref(), model_lock_sha='d' * 64,
        calibration=ref(), source_faces=ref(), donor_faces=ref())


def test_glare_stage_denies_before_reading_physical_pixels(monkeypatch, tmp_path):
    def forbidden(*args, **kwargs):
        pytest.fail('material IO must not start without live authority')
    monkeypatch.setattr(glare_stage, 'read_donor_image_material', forbidden)
    mask = np.ones((8, 8), np.float32)
    with pytest.raises(ValueError, match='GLARE_DONOR_AUTHORITY_UNAVAILABLE'):
        glare_stage.prepare_and_stage_glare_donor(
            params(), b'source', b'donor', b'proof', b'faces', b'faces',
            object(), mask, np.zeros_like(mask), object(), object(),
            source_origin=(0, 0), min_face_edge_px=32,
            material_scratch_root=tmp_path, material_max_bytes=1024,
            check_active=lambda: None, authority=lambda *_: False,
            alpha_mode='require_float32')


def test_glare_full_mask_plans_halo_and_passes_complete_patch(monkeypatch, tmp_path):
    canvas = SimpleNamespace(width=20, height=20)
    monkeypatch.setattr(glare_stage, 'verify_donor_image_pair',
                        lambda *a: (SimpleNamespace(canvas=canvas),
                                    SimpleNamespace(canvas=canvas)))
    monkeypatch.setattr(glare_stage, 'verify_glare_face_geometry',
                        lambda *a: SimpleNamespace(source_diagonal=20.))
    monkeypatch.setattr(glare_stage, 'admit_donor_portrait',
                        lambda profile, *dims: {'material_scratch_bytes': 1})
    received = {}

    def stage(*args, **kwargs):
        received['origin'] = kwargs['source_origin']
        received['mask'] = args[7]
        return 'staged'

    monkeypatch.setattr(glare_stage, 'prepare_and_stage_glare_donor', stage)
    target = np.zeros((20, 20), np.float32)
    target[10:12, 10:12] = 1
    result = glare_stage.prepare_and_stage_glare_full_mask(
        params(), b'source', b'donor', b'proof', b'faces', b'faces',
        object(), target, np.zeros_like(target), object(), object(),
        profile={}, min_face_edge_px=32, material_scratch_root=tmp_path,
        material_max_bytes=1024, check_active=lambda: None,
        authority=lambda *_: True, alpha_mode='require_float32')
    assert result == 'staged'
    x, y = received['origin']
    patch = received['mask']
    assert x <= 10 < x + patch.shape[1] and y <= 10 < y + patch.shape[0]
    assert patch.sum() == 4 and patch.size <= 1_048_576
