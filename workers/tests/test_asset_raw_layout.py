"""Native RAW decoder tensors must match the RGB16 color-conversion ABI."""

import sys

import numpy as np
import pytest

from photo_worker.operations.asset.decode import validate_raw_rgb_layout
from photo_worker.operations.asset.verify import AssetError


def test_raw_rgb_layout_accepts_native_contiguous_tensor():
    rgb = np.zeros((3, 4, 3), dtype=np.uint16)
    assert validate_raw_rgb_layout(rgb) is rgb


@pytest.mark.parametrize('rgb', [
    np.zeros((3, 4, 3), dtype=np.uint16)[:, ::-1, :],
    np.zeros((3, 4, 3), dtype=np.uint16).transpose(1, 0, 2),
    np.zeros((3, 4, 4), dtype=np.uint16),
    np.zeros((3, 4, 3), dtype=np.uint8),
])
def test_raw_rgb_layout_refuses_wrong_stride_bands_or_dtype(rgb):
    with pytest.raises(AssetError, match='RAW_DECODE_CONTRACT_ERROR'):
        validate_raw_rgb_layout(rgb)


def test_raw_rgb_layout_refuses_non_native_or_unaligned_words():
    opposite = '>' if sys.byteorder == 'little' else '<'
    non_native = np.zeros((2, 2, 3), dtype=opposite + 'u2')
    unaligned = np.ndarray((2, 2, 3), dtype=np.uint16,
                           buffer=bytearray(25), offset=1)
    for rgb in (non_native, unaligned):
        with pytest.raises(AssetError, match='RAW_DECODE_CONTRACT_ERROR'):
            validate_raw_rgb_layout(rgb)
