import numpy as np
import pytest

from photo_worker.algorithms.mask_png import png16_bound, write_png16
from photo_worker.algorithms.mask_png_verify import verify_png16


def fixture(path):
    mask = np.random.default_rng(7).random((8, 40000), dtype=np.float32)
    with path.open('wb') as stream:
        write_png16(mask, stream, max_bytes=png16_bound(40000, 8))
    return mask


def test_verify_png16_compares_multiple_chunks_and_row_blocks(tmp_path):
    path = tmp_path/'mask.png'
    mask = fixture(path)
    verify_png16(mask, path)
    wrong = mask.copy()
    wrong[-1, -1] = 0.
    with pytest.raises(ValueError, match='differs'):
        verify_png16(wrong, path)


@pytest.mark.parametrize('corruption', ['crc', 'truncated', 'trailing'])
def test_verify_png16_refuses_corrupted_physical_file(tmp_path, corruption):
    path = tmp_path/'mask.png'
    mask = fixture(path)
    body = bytearray(path.read_bytes())
    if corruption == 'crc':
        body[-1] ^= 1
    elif corruption == 'truncated':
        body = body[:-1]
    else:
        body += b'\0'
    path.write_bytes(body)
    with pytest.raises(ValueError):
        verify_png16(mask, path)


def test_verify_png16_stops_on_cancellation(tmp_path):
    path = tmp_path/'mask.png'
    mask = fixture(path)
    checks = 0
    def check():
        nonlocal checks
        checks += 1
        if checks == 20:
            raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        verify_png16(mask, path, check_active=check)
    assert checks == 20


@pytest.mark.parametrize('kind', ['symlink', 'hardlink', 'fifo', 'directory'])
def test_verify_png16_refuses_nonowned_regular_file(tmp_path, kind):
    import os
    original = tmp_path/'original.png'
    mask = fixture(original)
    path = tmp_path/'input.png'
    if kind == 'symlink':
        path.symlink_to(original)
    elif kind == 'hardlink':
        os.link(original, path)
    elif kind == 'fifo':
        os.mkfifo(path)
    else:
        path.mkdir()
    with pytest.raises(ValueError, match='Invalid PNG file'):
        verify_png16(mask, path)


def test_verify_png16_refuses_empty_canvas_before_open(tmp_path):
    with pytest.raises(ValueError, match='dimensions'):
        verify_png16(np.zeros((0, 1), dtype=np.float32), tmp_path/'absent.png')
