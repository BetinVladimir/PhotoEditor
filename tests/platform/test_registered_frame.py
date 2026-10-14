from uuid import uuid4
import numpy as np
import pytest
from photo_worker.algorithms.panorama_geometry import write_registration_inverse
from photo_worker.operations.optical.registered_frame import prepare_registered_frame, registered_frame_resources
from photo_worker.runtime.scratch import ScratchSpace


def test_translation_field_has_half_pixel_centres_and_zero_uncovered():
    field = np.empty((5, 7, 2), np.float32); coverage = np.empty((5, 7), bool)
    matrix = np.array([[1., 0, 2], [0, 1, 1], [0, 0, 1]])
    write_registration_inverse(matrix, (3, 4), field, coverage, tile=2)
    assert coverage[1:4, 2:6].all() and coverage.sum() == 12
    np.testing.assert_array_equal(field[1, 2], [.5, .5])
    np.testing.assert_array_equal(field[3, 5], [3.5, 2.5])
    assert not field[~coverage].any()


def test_registered_frame_identity_preserves_hdr_and_closes_owned_files(tmp_path):
    rgb = np.arange(5*7*3, dtype=np.float32).reshape(5, 7, 3)-20
    alpha = np.ones((5, 7), np.float32); required = registered_frame_resources((5, 7), (5, 7), tile=2)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, required['scratch_bytes'], min_free_bytes=0) as scratch:
        with prepare_registered_frame(rgb, alpha, np.eye(3), (5, 7), scratch,
                ram_max_bytes=required['ram_bytes'], tile=2) as files:
            np.testing.assert_array_equal(np.load(files.rgb), rgb)
            np.testing.assert_array_equal(np.load(files.alpha), alpha)
            assert np.load(files.coverage).all() and not files.source_to_output.flags.writeable
            files.reservation.validate(); path = files.rgb
        assert not path.exists()


def test_registration_ram_gate_precedes_files():
    class NoScratch:
        def reserve(self, _size): pytest.fail('unexpected allocation')
    with pytest.raises(ValueError, match='REGISTERED_FRAME_RAM_EXCEEDED'):
        with prepare_registered_frame(np.ones((3, 4, 3), np.float32), np.ones((3, 4), np.float32),
                np.eye(3), (3, 4), NoScratch(), ram_max_bytes=1):
            pytest.fail('unexpected result')
