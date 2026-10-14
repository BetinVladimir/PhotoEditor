from hashlib import sha256
import numpy as np
import pytest
from photo_worker.operations.interchange.export_array_probe import probe_export_array


def request_for(tmp_path):
    path = tmp_path / 'rgb.npy'
    np.save(path, np.zeros((1, 2, 3), dtype='<f4'), allow_pickle=False)
    raw = path.read_bytes()
    return dict(schema_version=1, file_path=str(path), sha256=sha256(raw).hexdigest(),
        bytes=len(raw), media_type='application/octet-stream', width=2, height=1,
        canvas_hash='b' * 64, contract='WorkingPixels', dtype='float32')


@pytest.mark.parametrize('media', ['application/octet-stream', 'application/x-npy'])
def test_array_probe_binds_actual_file_contract_and_canvas(monkeypatch, tmp_path, media):
    import photo_worker.operations.interchange.export_array_probe as module
    monkeypatch.setattr(module, 'export_mask_runtime_sha', lambda: 'c' * 64)
    request = request_for(tmp_path)
    request['media_type'] = media
    result = probe_export_array(request)
    assert result['media_type'] == media
    assert result['contract'] == 'WorkingPixels' and result['dtype'] == 'float32'
    assert result['sha256'] == request['sha256']
    assert result['canvas_hash'] == request['canvas_hash']
    assert result['runtime_sha256'] == 'c' * 64 and 'file_path' not in result


@pytest.mark.parametrize('patch', [dict(schema_version=True), dict(contract='MaskPixels'),
    dict(dtype='uint16'), dict(canvas_hash='invalid'), dict(width=True),
    dict(media_type='application/json'), dict(media_type='application/x-npy; charset=utf-8'), dict(approved=True), dict(file_path='relative.npy'), dict(sha256='a' * 64)])
def test_array_probe_rejects_substitution(tmp_path, patch):
    request = request_for(tmp_path);request.update(patch)
    with pytest.raises(ValueError):
        probe_export_array(request)


@pytest.mark.parametrize('size', [33, 65])
@pytest.mark.parametrize('mode', ['valid', 'shape', 'range', 'dtype', 'grid'])
def test_native_array_probe_qualifies_absolute_lut(tmp_path, size, mode):
    values = np.full((size, size, size, 3), .5, dtype=np.float32)
    if mode == 'shape': values = values[:, :, :, :2]
    if mode == 'range': values[0, 0, 0, 0] = 2
    if mode == 'dtype': values = values.astype(np.float64)
    path = tmp_path / 'lut.npy'; np.save(path, values, allow_pickle=False)
    raw = path.read_bytes()
    request = dict(schema_version=1, file_path=str(path), sha256=sha256(raw).hexdigest(),
        bytes=len(raw), media_type='application/x-npy', width=size, height=size,
        canvas_hash='b' * 64, contract='AbsoluteLUTArray', dtype='float32')
    if mode == 'grid': request['height'] = size - 1
    if mode == 'valid':
        result = probe_export_array(request)
        assert result['contract'] == 'AbsoluteLUTArray' and result['width'] == size
        assert result['sha256'] == request['sha256'] and result['dtype'] == 'float32'
    else:
        with pytest.raises(ValueError): probe_export_array(request)
