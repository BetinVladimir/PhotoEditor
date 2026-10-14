from hashlib import sha256

import numpy as np
import pytest

from photo_worker.operations.interchange.export_mask_qualification import qualify_export_mask_file


def mask_file(tmp_path, values):
    path = tmp_path / 'mask.npy'
    np.save(path, values, allow_pickle=False)
    raw = path.read_bytes()
    return path, len(raw), sha256(raw).hexdigest()


def test_export_mask_qualifies_actual_npy(tmp_path):
    path, size, digest = mask_file(tmp_path, np.array([[0, .5, 1]], dtype='<f4'))
    assert qualify_export_mask_file(path, 3, 1, size, digest) == {
        'sha256': digest, 'bytes': size, 'width': 3, 'height': 1, 'dtype': 'float32'}


@pytest.mark.parametrize('values', [
    np.array([[0, 1]], dtype='<u2'), np.array([[0, 1]], dtype='>f4'),
    np.array([[0, float('nan')]], dtype='<f4'),
    np.array([[0, float('inf')]], dtype='<f4'),
    np.array([[0, 1.01]], dtype='<f4'), np.array([[-.01, 1]], dtype='<f4'),
    np.array([[0], [1]], dtype='<f4'),
])
def test_export_mask_rejects_invalid_layout_and_values(tmp_path, values):
    path, size, digest = mask_file(tmp_path, values)
    with pytest.raises(ValueError):
        qualify_export_mask_file(path, 2, 1, size, digest)


def test_export_mask_rejects_hash_and_symlink(tmp_path):
    path, size, digest = mask_file(tmp_path, np.zeros((1, 2), dtype='<f4'))
    with pytest.raises(ValueError, match='MASK_HASH'):
        qualify_export_mask_file(path, 2, 1, size, 'a' * 64)
    link = tmp_path / 'link.npy'
    link.symlink_to(path)
    with pytest.raises(ValueError, match='MASK_FILE'):
        qualify_export_mask_file(link, 2, 1, size, digest)


def test_export_mask_cancellation_propagates(tmp_path):
    path, size, digest = mask_file(tmp_path, np.zeros((1, 2), dtype='<f4'))
    def cancelled():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        qualify_export_mask_file(path, 2, 1, size, digest, check_active=cancelled)
