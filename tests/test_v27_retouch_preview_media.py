"""Own soft-mask retouch media, using isolated display conversion doubles."""
from hashlib import sha256
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.semantic import preview_retouch_media as module
from photo_worker.operations.semantic.preview_retouch_loader import RetouchPreviewArrays
from test_v27_retouch_preview_binding import retouch_fixture
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('failure', [None, 'outside', 'protected', 'runtime', 'late_admission'])
def test_retouch_media_keeps_soft_coverage_and_independent_preservation(tmp_path, monkeypatch, failure):
    params, candidate, intent = retouch_fixture()
    edit, _, source = source_fixture()
    params['source'] = edit['source']
    doc = parse_json(candidate);doc['target'] = params['source']
    candidate = canonical_bytes(doc);params['candidate']['sha256'] = sha256(candidate).hexdigest()
    rgb = np.full((1,2,3), .4, np.float32)
    mask = np.array([[.25,0]],np.float32)
    protection = np.zeros((1,2),np.float32)
    if failure == 'protected':protection[0,0] = .01
    arrays = RetouchPreviewArrays(rgb,mask,protection,64*1048576,0)
    changed = rgb.copy();changed[0,1 if failure == 'outside' else 0] = .5
    calls = []
    def display(image, pixels, alpha, **kwargs):
        calls.append(pixels.copy())
        return object()
    monkeypatch.setattr(module,'prepare_exposure_display',display)
    def run():return module.prepare_retouch_preview_media(params,candidate,intent,source,arrays,changed,None,
        icc_root=tmp_path,icc_lock_sha='a'*64,srgb_sha='b'*64,max_bytes=256*1048576,
        verify_admission=lambda _:not (failure == 'late_admission' and calls),
        verify_runtime=lambda:failure != 'runtime',verify_color=lambda _:True,check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError):run()
        assert len(calls) == (1 if failure == 'late_admission' else 0)
    else:
        result = run()
        np.testing.assert_array_equal(calls[0],rgb)
        np.testing.assert_array_equal(calls[1],changed)
        assert result.affected[0,0,3] == 64 and result.affected[0,1,3] == 0
        assert not result.protected[:,:,3].any()
        np.testing.assert_array_equal(arrays.mask,mask)
