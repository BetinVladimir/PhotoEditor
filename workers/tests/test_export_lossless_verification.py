from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.operations.export.encode import (
    _verify, _verify_lossless_pixels, _write)


@pytest.mark.parametrize('format_name,suffix', [('png', '.png'),
                                                 ('tiff', '.tiff')])
def test_lossless_export_round_trip_detects_pixel_substitution(
        tmp_path, format_name, suffix):
    profile = (Path(__file__).resolve().parents[2] /
               'resources/icc/srgb_v1.icc').read_bytes()
    params = SimpleNamespace(format=format_name, quality=None,
                             icc_sha256=sha256(profile).hexdigest())
    pixels = np.array([
        [[.1, .2, .3], [.4, .5, .6], [.7, .8, .9]],
        [[.91, .81, .71], [.61, .51, .41], [.31, .21, .11]],
    ], dtype=np.float32)
    path = tmp_path / ('encoded' + suffix)
    _write(path, pixels, profile, params)
    _verify(path, params, 3, 2, False, expected_pixels=pixels)
    substituted = pixels.copy()
    substituted[0, 1, 0] = .99
    _write(path, substituted, profile, params)
    with pytest.raises(ValueError, match='ENCODE_PIXEL_MISMATCH'):
        _verify(path, params, 3, 2, False, expected_pixels=pixels)


def test_lossless_pixel_guard_checks_alpha_channel_exactly():
    pixels = np.array([[[.1, .2, .3, .4]]], dtype=np.float32)
    decoded = np.round(pixels * 65535).astype(np.uint16)
    _verify_lossless_pixels(decoded, pixels, (0, 1, 2, 3), lambda: None)
    decoded[0, 0, 3] += 1
    with pytest.raises(ValueError, match='ENCODE_PIXEL_MISMATCH'):
        _verify_lossless_pixels(decoded, pixels, (0, 1, 2, 3), lambda: None)
