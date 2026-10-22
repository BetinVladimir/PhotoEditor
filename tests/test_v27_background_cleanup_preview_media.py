import numpy as np
import pytest
from test_v27_background_cleanup_preview import background_fixture
from test_v27_exposure_source import source_fixture
from photo_worker.operations.semantic.preview_background_cleanup_loader import BackgroundCleanupPreviewArrays
from photo_worker.operations.semantic import preview_background_cleanup_media as module


def setup():
    params, candidate, intent = background_fixture()
    edit, image, raw = source_fixture(); params['source'] = edit['source']
    rgb = np.full((1, 2, 3), .4, np.float32)
    masks = np.array([[1, 0]], np.float32)
    arrays = BackgroundCleanupPreviewArrays(rgb, masks, np.zeros((1, 2), np.float32), 64*1048576, 0)
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
    result=module.prepare_background_cleanup_preview_media(params,candidate,intent,raw,arrays,changed,None,**options(tmp_path))
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
        module.prepare_background_cleanup_preview_media(params,candidate,intent,raw,arrays,changed,None,**options(tmp_path))


@pytest.mark.parametrize('boundary', ['before', 'after', 'affected', 'protected'])
@pytest.mark.parametrize('loss', ['cancel', 'admission', 'runtime'])
def test_cleanup_media_stops_between_each_display_and_overlay(tmp_path, monkeypatch, boundary, loss):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes, parse_json
    params, candidate, intent, raw, arrays = setup()
    body = parse_json(candidate)
    body['target'] = params['source']
    candidate = canonical_bytes(body)
    params['candidate']['sha256'] = sha256(candidate).hexdigest()
    changed = arrays.original.copy()
    changed[0, 0] = .5
    calls = []
    lost = False
    def mark(phase):
        nonlocal lost
        calls.append(phase)
        if phase == boundary:
            lost = True
    def display(image, rgb, alpha, **kwargs):
        mark('before' if not calls else 'after')
        return object()
    def overlay(mask, **kwargs):
        mark('protected' if kwargs['protected'] else 'affected')
        return object()
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during media preparation')
    opts = options(tmp_path)
    opts.update(verify_admission=lambda _: not (lost and loss == 'admission'),
        verify_runtime=lambda: not (lost and loss == 'runtime'), check_active=active)
    monkeypatch.setattr(module, 'prepare_exposure_display', display)
    monkeypatch.setattr(module, 'prepare_preview_mask_overlay', overlay)
    with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
        module.prepare_background_cleanup_preview_media(params, candidate, intent, raw,
            arrays, changed, None, **opts)
    phases = ['before', 'after', 'affected', 'protected']
    assert calls == phases[:phases.index(boundary)+1]
    np.testing.assert_array_equal(arrays.original, np.full((1, 2, 3), .4, np.float32))
    np.testing.assert_array_equal(arrays.mask, np.array([[1, 0]], np.float32))


@pytest.mark.parametrize('field', ['original', 'encoded', 'mask', 'protected'])
def test_cleanup_media_rejects_nonfinite_arrays_before_any_display(tmp_path, monkeypatch, field):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes, parse_json
    params, candidate, intent, raw, arrays = setup()
    body = parse_json(candidate)
    body['target'] = params['source']
    candidate = canonical_bytes(body)
    params['candidate']['sha256'] = sha256(candidate).hexdigest()
    changed = arrays.original.copy()
    value = changed if field == 'encoded' else getattr(arrays, field)
    value.flat[0] = np.nan
    def forbidden(*a, **k):
        raise AssertionError('display reached with nonfinite numerical evidence')
    monkeypatch.setattr(module, 'prepare_exposure_display', forbidden)
    with pytest.raises(ValueError):
        module.prepare_background_cleanup_preview_media(params, candidate, intent, raw,
            arrays, changed, None, **options(tmp_path))
