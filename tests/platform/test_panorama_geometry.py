import numpy as np
import pytest
from photo_worker.algorithms.panorama_geometry import plan_panorama_canvas, write_cylindrical_inverse


def test_canvas_contains_full_pixel_edges_and_translates_negative_offsets():
    h = np.array([[1., 0, -20], [0, 1, 7], [0, 0, 1]])
    canvas = plan_panorama_canvas([np.eye(3), h], [(40, 60)]*2, max_pixels=10000)
    assert (canvas.width, canvas.height, canvas.left, canvas.top) == (80, 47, -20, 0)
    np.testing.assert_array_equal(canvas.source_to_output[0], [[1, 0, 20], [0, 1, 0], [0, 0, 1]])
    assert not canvas.source_to_output[0].flags.writeable


def test_canvas_rejects_cap_before_image_allocation():
    with pytest.raises(ValueError, match='PANORAMA_PIXEL_LIMIT'):
        plan_panorama_canvas([np.eye(3)]*2, [(100, 100)]*2, max_pixels=9999)


def test_identity_canvas_has_no_extra_border():
    canvas = plan_panorama_canvas([np.eye(3)]*2, [(19, 31)]*2, max_pixels=589)
    assert (canvas.width, canvas.height) == (31, 19)


def test_cylindrical_map_uses_half_pixel_centres_and_explicit_coverage():
    field = np.empty((51, 101, 2), dtype=np.float32); coverage = np.empty((51, 101), dtype=bool)
    write_cylindrical_inverse(field, coverage, tile_rows=7)
    np.testing.assert_array_equal(field[25, 50], [50.5, 25.5])
    assert coverage[25, 50] and not coverage[:, 0].any() and not coverage[:, -1].any()
    assert np.isfinite(field).all() and np.all(field[~coverage] == 0)
    again = np.empty_like(field); valid = np.empty_like(coverage)
    write_cylindrical_inverse(again, valid, tile_rows=19)
    np.testing.assert_array_equal(field, again); np.testing.assert_array_equal(coverage, valid)


def test_cylindrical_cancellation_propagates():
    def cancel():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        write_cylindrical_inverse(np.empty((8, 8, 2), dtype=np.float32), np.empty((8, 8), dtype=bool), check_active=cancel)
