"""Actual existing CPU beauty algorithm on synthetic faces, not photo qualification."""
import json
from pathlib import Path
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
from photo_worker.operations.semantic.preview_retouch_compute import compute_retouch_preview
from photo_worker.runtime.scratch import ScratchSpace
from test_beauty_prepare import inputs


def real_compute_inputs(with_alpha=False):
    from hashlib import sha256
    rgb, image, entries, _ = inputs()
    from photo_worker.contracts.base import ArtifactRef
    alpha = None
    if with_alpha:
        image.alpha = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
        alpha = np.linspace(0, 1, image.canvas.width * image.canvas.height, dtype=np.float32).reshape(image.canvas.height, image.canvas.width)
    root = Path(__file__).resolve().parents[2] / 'backend/internal/semanticreview/testdata'
    params = json.loads((root / 'retouch_preview_params.json').read_bytes())
    context = json.loads((root / 'retouch_preview_context.json').read_bytes())
    params['source'] = dict(asset_version_id=str(uuid4()),
        source=dict(artifact_id=str(uuid4()), sha256=sha256(canonical_bytes(image)).hexdigest()),
        canvas_hash=canonical_hash(image.canvas))
    context.update(source=params['source'], candidate=params['candidate'], protection=params['protection'])
    manual = dict(context['faces'][0]['manual'])
    manual.update(canvas=image.canvas.model_dump(mode='json'),
        transform_chain=image.transform_chain.model_dump(mode='json'), object_id=None)
    context['faces'] = [dict(landmarks=dict(artifact_id=str(uuid4()), sha256='f' * 64),
        object_id=None, skin=None, manual=manual, manual_mode=True) for entry in entries]
    owned = SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    bound = RetouchPreviewAdapterContext.model_validate_json(canonical_bytes(context))
    height, width = image.canvas.height, image.canvas.width
    mask = np.ones((height, width), np.float32)
    mask[:, :16] = 0
    protected = np.zeros((height, width), np.float32)
    protected[:16, :] = 1
    return owned, bound, image, rgb, mask, protected, entries, alpha


@pytest.mark.parametrize('strength', [0., .25, .5, 1.])
@pytest.mark.parametrize('consumer_failure', [False, True])
@pytest.mark.parametrize('with_alpha', [False, True])
def test_real_retouch_cpu_compute_preserves_regions_and_cleans_scratch(tmp_path, strength, consumer_failure, with_alpha):
    owned, bound, image, rgb, mask, protected, entries, alpha = real_compute_inputs(with_alpha)
    frozen = rgb.copy()
    frozen_alpha = None if alpha is None else alpha.copy()
    def admission(value, binding):
        assert value == owned and binding == bound
        return True
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 20 * 1024**2, min_free_bytes=0) as scratch:
        def run():
            with compute_retouch_preview(owned, bound, image, rgb, mask, protected,
                    tuple(entry.face for entry in entries), scratch, strength=strength,
                    max_ram_bytes=2**31, verify_admission=admission, check_active=lambda: None, alpha=alpha) as result:
                assert len(result.faces) == len(entries)
                assert result.preservation.alpha_unchanged
                if with_alpha:
                    np.testing.assert_array_equal(alpha.view(np.uint32), frozen_alpha.view(np.uint32))
                assert result.preservation.protected_unchanged
                assert result.preservation.outside_mask_unchanged
                np.testing.assert_array_equal(result.pixels[:, :16], frozen[:, :16])
                np.testing.assert_array_equal(result.pixels[:16], frozen[:16])
                if strength == 0:
                    np.testing.assert_array_equal(result.pixels, frozen)
                    assert result.preservation.changed_pixels == 0
                else:
                    assert any(report.applied for report in result.faces)
                if consumer_failure:
                    raise RuntimeError('consumer failed')
        if consumer_failure:
            with pytest.raises(RuntimeError, match='consumer failed'): run()
        else:
            run()
        assert not list(scratch.directory.iterdir())
    np.testing.assert_array_equal(rgb, frozen)


@pytest.mark.parametrize('phase', ['face_delta', 'output_open', 'consumer_exit'])
@pytest.mark.parametrize('loss', ['cancel', 'lease', 'admission'])
def test_real_compute_releases_maps_and_scratch_after_late_authority_loss(tmp_path, phase, loss):
    from photo_worker.runtime.executor import TaskCancelled, LeaseLost
    owned, bound, image, rgb, mask, protected, entries, alpha = real_compute_inputs(True)
    revoked = False
    reached = False
    yielded = []
    expected = TaskCancelled if loss == 'cancel' else LeaseLost if loss == 'lease' else ValueError
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 20 * 1024**2, min_free_bytes=0) as scratch:
        def expired():
            nonlocal revoked, reached
            if phase != 'consumer_exit':
                prefix = 'face-delta-' if phase == 'face_delta' else 'beauty-rgb-'
                if any(path.name.startswith(prefix) and path.stat().st_size > 8192
                    for path in scratch.directory.iterdir()):
                    revoked = reached = True
            return revoked
        def active():
            if expired() and loss != 'admission':
                raise expected('authority revoked')
        def admission(value, binding):
            assert value == owned and binding == bound
            return not expired() if loss == 'admission' else True
        with pytest.raises(expected):
            with compute_retouch_preview(owned, bound, image, rgb, mask, protected,
                    tuple(entry.face for entry in entries), scratch, strength=.5,
                    max_ram_bytes=2**31, verify_admission=admission,
                    check_active=active, alpha=alpha) as result:
                assert phase == 'consumer_exit'
                assert result.path.is_file() and not result.pixels._mmap.closed
                yielded.append((result.path, result.pixels._mmap))
                revoked = reached = True
        assert reached
        assert len(yielded) == (1 if phase == 'consumer_exit' else 0)
        assert all(not path.exists() and mapping.closed for path, mapping in yielded)
        assert not list(scratch.directory.iterdir())
