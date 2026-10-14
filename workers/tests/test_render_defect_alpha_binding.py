"""Legacy alpha must retain exact source binding for reviewed repair."""
import numpy as np

from photo_worker.v2.render_defects import alpha_tile_matches


def test_reviewed_defect_alpha_accepts_exact_normalized_legacy_tile():
    source = np.array([[0, 1, 32768, 65535]], dtype=np.uint16)
    normalized = source.astype(np.float32) / np.float32(65535)
    assert alpha_tile_matches(normalized, source)
    changed = normalized.copy()
    changed[0, 2] = np.nextafter(changed[0, 2], np.float32(1))
    assert not alpha_tile_matches(changed, source)


def test_reviewed_defect_alpha_rejects_changed_float32_bits_and_dtype():
    source = np.array([[0.0, 0.5, 1.0]], dtype=np.float32)
    assert alpha_tile_matches(source.copy(), source)
    changed = source.copy()
    changed[0, 1] = np.nextafter(changed[0, 1], np.float32(1))
    assert not alpha_tile_matches(changed, source)
    assert not alpha_tile_matches(source.astype(np.float64), source)
