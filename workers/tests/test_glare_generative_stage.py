"""Donorless glare staging refuses absent live consent/model authority."""

from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.portrait import glare_generative_stage


def ref():
    return {'artifact_id': uuid4(), 'sha256': 'a' * 64}


def params():
    return PortraitGlareParams(schema_version=1,
        base={'asset_version_id': uuid4(), 'source': ref(),
              'canvas_hash': 'b' * 64}, strength=.5, donor=None,
        glasses_mask=ref(), protected=ref(), mode='generative',
        confirmation=ref(), model_lock_sha='c' * 64,
        calibration=ref(), source_faces=ref(), donor_faces=None)


def test_generative_glare_stage_denies_before_model_work(monkeypatch):
    monkeypatch.setattr(glare_generative_stage, 'prepare_generative_glare_patch',
                        lambda *a, **kw: pytest.fail('inference must not run'))
    image = np.zeros((8, 8, 3), np.float32)
    mask = np.ones((8, 8), np.float32)
    with pytest.raises(ValueError, match='GLARE_GENERATIVE_AUTHORITY_UNAVAILABLE'):
        glare_generative_stage.prepare_and_stage_generative_glare(
            params(), b'source', b'proof', b'faces', image, mask,
            np.zeros_like(mask), object(), object(), object(),
            min_face_edge_px=32, check_active=lambda: None,
            authority=lambda *_: False)
