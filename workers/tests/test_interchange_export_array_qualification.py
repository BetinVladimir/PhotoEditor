from hashlib import sha256
import numpy as np
import pytest
from photo_worker.operations.interchange.export_mask_qualification import qualify_export_array_file


def qualify(tmp_path, contract, values, dtype):
    path = tmp_path / 'array.npy'
    np.save(path, values, allow_pickle=False)
    raw = path.read_bytes()
    return qualify_export_array_file(path, contract, 2, 1, len(raw),
        sha256(raw).hexdigest(), dtype=dtype)


@pytest.mark.parametrize('contract,values,dtype', [
    ('WorkingPixels', np.array([[[-.1, 1.2, 0], [0, 1, .5]]], dtype='<f4'), 'float32'),
    ('PixelArray', np.zeros((1, 2, 3), dtype='<u2'), 'uint16'),
    ('WorkingAlpha', np.array([[0, 65535]], dtype='<u2'), 'float32'),
    ('WorkingAlpha', np.array([[0, 1]], dtype='<f4'), 'float32'),
    ('DenseWarpField', np.array([[[-100, 200], [1, 2]]], dtype='<f4'), 'float32'),
    ('DenseWarpArray', np.zeros((1, 2, 2), dtype='<f4'), 'float32'),
])
def test_export_array_preserves_existing_encodings_and_ranges(tmp_path, contract, values, dtype):
    receipt = qualify(tmp_path, contract, values, dtype)
    assert receipt['dtype'] == str(values.dtype)


@pytest.mark.parametrize('contract,values,dtype', [
    ('WorkingPixels', np.zeros((1, 2), dtype='<f4'), 'float32'),
    ('WorkingPixels', np.full((1, 2, 3), np.nan, dtype='<f4'), 'float32'),
    ('WorkingPixels', np.zeros((1, 2, 3), dtype='>f4'), 'float32'),
    ('WorkingAlpha', np.array([[0, 1.1]], dtype='<f4'), 'float32'),
    ('DenseWarpField', np.full((1, 2, 2), np.inf, dtype='<f4'), 'float32'),
    ('DenseWarpArray', np.zeros((1, 2, 2), dtype='<u2'), 'uint16'),
    ('Artifact', np.zeros((1, 2), dtype='<f4'), 'float32'),
])
def test_export_array_rejects_invalid_contract_layout_values(tmp_path, contract, values, dtype):
    with pytest.raises(ValueError):
        qualify(tmp_path, contract, values, dtype)


@pytest.mark.parametrize('contract,shape', [
    ('WorkingPixels', (1, 2, 3)), ('WorkingAlpha', (1, 2)),
    ('DenseWarpField', (1, 2, 2)), ('MaskPixels', (1, 2)),
])
@pytest.mark.parametrize('version,prefix_size', [(1, 10), (2, 12)])
def test_export_array_accepts_maximum_header_with_npy_prefix(tmp_path, contract, shape, version, prefix_size):
    import struct
    from photo_worker.runtime.arrays import MAX_HEADER
    values = np.zeros(shape, dtype='<f4')
    header = repr(dict(descr='<f4', fortran_order=False, shape=shape)).encode('ascii')
    header = header + b' ' * (MAX_HEADER - len(header) - 1) + b'\n'
    raw = (b'\x93NUMPY' + bytes((version, 0))
           + struct.pack('<H' if version == 1 else '<I', MAX_HEADER)
           + header + values.tobytes())
    path = tmp_path / 'maximum-header.npy'
    path.write_bytes(raw)
    assert len(raw) == prefix_size + MAX_HEADER + values.nbytes
    result = qualify_export_array_file(path, contract, 2, 1, len(raw),
        sha256(raw).hexdigest(), dtype='float32')
    assert result['bytes'] == len(raw) and result['dtype'] == 'float32'
