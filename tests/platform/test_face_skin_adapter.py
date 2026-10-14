from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.face_parse import FaceLabels19
from photo_worker.operations.mask.face_skin import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_image_color_adapter import context
from test_mask_raster_adapter import MaskArtifacts


def setup(root, with_skin=True):
    artifacts = MaskArtifacts(root)
    labels = np.zeros((39, 601), dtype=np.uint16)
    if with_skin:
        labels[2:5, 10:20] = 1
    labels[5:8, 10:20] = 2
    pixels = artifacts.array(root, 'face19', labels)
    manifest = FaceLabels19(image=artifacts.image_ref, faces=artifacts.image_ref,
        labels=pixels, canvas=artifacts.image.canvas,
        transform_chain=artifacts.image.transform_chain, model_id='bisenet_face19',
        model_lock_id='a'*64, backend='cpu', preprocessing_id='bisenet_face19_crop512_v1',
        parsed_faces=1, mean_confidence=.9, warnings=[])
    ref = artifacts.add(manifest)
    artifacts.raw = dict(labels=ref.model_dump(mode='json'), algorithm='bisenet_face19_skin_v1')
    ctx = context(artifacts)
    ctx.lease.execution_profile['backend'] = 'cpu'
    ctx.lease.command.inputs.extend([ref, pixels])
    return artifacts, ctx, labels, manifest


@pytest.mark.parametrize('with_skin', [True, False])
def test_skin_worker_publishes_exact_bound_triplet(tmp_path, with_skin):
    artifacts, ctx, labels, parsed = setup(tmp_path, with_skin)
    root = tmp_path/'scratch'
    root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 12*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        result = execute(ctx, artifacts)
        assert not list(scratch.directory.iterdir())
    expected = (labels == 1).astype(np.float32)
    np.testing.assert_array_equal(artifacts.outputs['skin_mask'], expected)
    np.testing.assert_array_equal(artifacts.outputs['skin_mask_png'], expected.astype(np.uint16)*65535)
    role, mask = artifacts.staged[0]
    assert role == 'skin_mask_ref' and len(result.outputs) == 3
    assert mask.artifact == result.outputs[0]
    assert mask.canvas == parsed.canvas and mask.transform_chain == parsed.transform_chain
    assert mask.selector == 'skin' and not mask.protected
    assert mask.model_lock_id == parsed.model_lock_id
    assert mask.confidence == (.9 if with_skin else 0.)
    assert result.review_required is (not with_skin)
    assert result.bytes_written > 0
    assert ctx.snapshot_progress() == dict(done=3, total=3)


def test_skin_worker_refuses_undeclared_binary_before_staging(tmp_path):
    artifacts, ctx, _, parsed = setup(tmp_path)
    ctx.lease.command.inputs.remove(parsed.labels)
    with pytest.raises(OperationFailure) as failure:
        execute(ctx, artifacts)
    assert failure.value.error.code == 'INPUT_INVALID'
    assert not artifacts.uploads and not artifacts.staged


def test_skin_worker_cancellation_cleans_scratch_before_staging(tmp_path):
    artifacts, ctx, _, _ = setup(tmp_path)
    root = tmp_path/'scratch'
    root.mkdir()
    checks = 0
    def cancel_during_conversion():
        nonlocal checks
        checks += 1
        if checks == 10:
            raise RuntimeError('cancelled')
    ctx.check_active = cancel_during_conversion
    with ScratchSpace(root, uuid4(), uuid4(), 1, 12*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with pytest.raises(RuntimeError, match='cancelled'):
            execute(ctx, artifacts)
        assert not list(scratch.directory.iterdir())
    assert not artifacts.uploads and not artifacts.staged


def test_skin_worker_refuses_corrupted_materialized_mask_before_staging(tmp_path, monkeypatch):
    import photo_worker.operations.mask.face_skin as operation
    original = operation.write_png16
    def corrupt_npy_after_png(mask, stream, **kwargs):
        result = original(mask, stream, **kwargs)
        paths = list(artifacts.scratch.directory.rglob('*.npy'))
        assert len(paths) == 1
        written = np.load(paths[0], mmap_mode='r+', allow_pickle=False)
        try:
            written[0, 0] = .5
            written.flush()
        finally:
            written._mmap.close()
        return result
    artifacts, ctx, _, _ = setup(tmp_path)
    monkeypatch.setattr(operation, 'write_png16', corrupt_npy_after_png)
    root = tmp_path/'scratch'
    root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 12*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with pytest.raises(OperationFailure) as failure:
            execute(ctx, artifacts)
        assert failure.value.error.code == 'INPUT_INVALID'
        assert not list(scratch.directory.iterdir())
    assert not artifacts.uploads and not artifacts.staged


def test_skin_worker_refuses_corrupted_png_before_staging(tmp_path, monkeypatch):
    import photo_worker.operations.mask.face_skin as operation
    original = operation.write_png16
    def corrupt_png(mask, stream, **kwargs):
        result = original(mask, stream, **kwargs)
        stream.write(b'\0')
        return result
    artifacts, ctx, _, _ = setup(tmp_path)
    monkeypatch.setattr(operation, 'write_png16', corrupt_png)
    root = tmp_path/'scratch'
    root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 12*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with pytest.raises(OperationFailure) as failure:
            execute(ctx, artifacts)
        assert failure.value.error.code == 'INPUT_INVALID'
        assert not list(scratch.directory.iterdir())
    assert not artifacts.uploads and not artifacts.staged
