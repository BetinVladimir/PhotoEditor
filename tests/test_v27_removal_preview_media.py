import numpy as np
import pytest
from test_v27_removal_preview_binding import removal_fixture
from test_v27_exposure_source import source_fixture
from photo_worker.operations.semantic.preview_removal_loader import RemovalPreviewArrays
from photo_worker.operations.semantic import preview_removal_media as module


def setup():
    params, candidate, intent = removal_fixture()
    edit, image, raw = source_fixture(); params['source'] = edit['source']
    rgb = np.full((1, 2, 3), .4, np.float32)
    masks = np.array([[1, 0]], np.float32)
    arrays = RemovalPreviewArrays(rgb, masks, np.zeros((1, 2), np.float32), 64*1048576, 0)
    return params, candidate, intent, raw, arrays


def options(tmp_path):
    return dict(icc_root=tmp_path, icc_lock_sha='a'*64, srgb_sha='b'*64,
        max_bytes=256*1048576, verify_admission=lambda _: True,
        verify_runtime=lambda: True, verify_color=lambda _: True, check_active=lambda: None)


def test_media_uses_owned_before_after_and_exact_editing_overlay(tmp_path, monkeypatch):
    params, candidate, intent, raw, arrays = setup()
    # Source substitution in this isolated media fixture also requires candidate binding.
    import json
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    body=json.loads(candidate); body['target']=params['source']
    candidate=canonical_bytes(body); params['candidate']['sha256']=sha256(candidate).hexdigest()
    seen=[]
    def display(image, rgb, alpha, **kwargs):
        seen.append(rgb.copy())
        return object()
    monkeypatch.setattr(module, 'prepare_exposure_display', display)
    changed=arrays.original.copy(); changed[0, 0]=.5
    result=module.prepare_removal_preview_media(params,candidate,intent,raw,arrays,changed,None,**options(tmp_path))
    np.testing.assert_array_equal(seen[0], arrays.original)
    np.testing.assert_array_equal(seen[1], changed)
    assert result.affected[0, 0, 3] == 255 and result.affected[0, 1, 3] == 0


def test_media_rejects_changed_pixels_outside_mask_before_display(tmp_path, monkeypatch):
    params,candidate,intent,raw,arrays=setup()
    import json
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    body=json.loads(candidate);body['target']=params['source']
    candidate=canonical_bytes(body);params['candidate']['sha256']=sha256(candidate).hexdigest()
    def unexpected(*args, **kwargs):
        raise AssertionError('display must not run')
    monkeypatch.setattr(module,'prepare_exposure_display',unexpected)
    changed=arrays.original.copy();changed[0,1]=.5
    with pytest.raises(ValueError):
        module.prepare_removal_preview_media(params,candidate,intent,raw,arrays,changed,None,**options(tmp_path))


@pytest.mark.parametrize('failure', ['color_revocation', 'copy_cancellation'])
def test_removal_media_stops_before_display_after_copy_or_color_loss(tmp_path, monkeypatch, failure):
    import json
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.runtime.executor import TaskCancelled
    params, candidate, intent, raw, arrays = setup()
    body = json.loads(candidate); body['target'] = params['source']
    candidate = canonical_bytes(body); params['candidate']['sha256'] = sha256(candidate).hexdigest()
    copied = []; allowed = True

    class TrackedPixels(np.ndarray):
        def copy(self, *args, **kwargs):
            copied.append(True)
            return super().copy(*args, **kwargs)

    arrays = RemovalPreviewArrays(arrays.original.view(TrackedPixels), arrays.mask,
        arrays.protected, arrays.loader_peak_bytes, arrays.retained_loader_bytes)
    def active():
        if failure == 'copy_cancellation' and copied:
            raise TaskCancelled('cancelled during media ownership copy')
    def color(_):
        nonlocal allowed
        allowed = False
        return True
    def unexpected(*args, **kwargs):
        raise AssertionError('display after loss of authority')
    monkeypatch.setattr(module, 'prepare_exposure_display', unexpected)
    args = options(tmp_path)
    args.update(verify_admission=lambda _: allowed, verify_color=color, check_active=active)
    with pytest.raises(TaskCancelled if failure == 'copy_cancellation' else ValueError):
        module.prepare_removal_preview_media(params, candidate, intent, raw, arrays,
            np.full((1, 2, 3), .4, np.float32), None, **args)
    assert copied == [True]
