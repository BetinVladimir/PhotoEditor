from hashlib import sha256
import numpy as np
import pytest
from photo_worker.operations.interchange.export_mask_probe import probe_export_mask


def request_for(tmp_path):
    path = tmp_path / 'mask.npy'
    np.save(path, np.array([[0, .5, 1]], dtype='<f4'), allow_pickle=False)
    raw = path.read_bytes()
    return dict(schema_version=1, file_path=str(path), sha256=sha256(raw).hexdigest(),
                bytes=len(raw), media_type='application/octet-stream',
                width=3, height=1, canvas_hash='b' * 64)


@pytest.mark.parametrize('media', ['application/octet-stream', 'application/x-npy'])
def test_mask_probe_qualifies_actual_file(monkeypatch, tmp_path, media):
    import photo_worker.operations.interchange.export_mask_probe as module
    monkeypatch.setattr(module, 'export_mask_runtime_sha', lambda: 'c' * 64)
    request = request_for(tmp_path)
    request['media_type'] = media
    result = probe_export_mask(request)
    assert result['media_type'] == media
    assert result['sha256'] == request['sha256']
    assert result['canvas_hash'] == request['canvas_hash']
    assert result['runtime_sha256'] == 'c' * 64
    assert result['dtype'] == 'float32' and 'file_path' not in result


@pytest.mark.parametrize('patch', [dict(schema_version=True), dict(file_path='relative.npy'),
    dict(media_type='image/png'), dict(media_type='application/x-npy; charset=utf-8'), dict(canvas_hash='bad'), dict(width=True),
    dict(approved=True), dict(height=2), dict(sha256='a' * 64)])
def test_mask_probe_rejects_substitution(tmp_path, patch):
    request = request_for(tmp_path)
    request.update(patch)
    with pytest.raises(ValueError):
        probe_export_mask(request)
