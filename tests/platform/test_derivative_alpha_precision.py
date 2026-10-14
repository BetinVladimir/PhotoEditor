"""Derivative intermediates retain float32 opacity rather than quantizing it."""
from contextlib import ExitStack
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.derivative import ResizeParams
from photo_worker.operations.derivative.common import open_image, stage_arrays
from photo_worker.runtime.scratch import ScratchSpace
from test_derivative_adapters import Artifacts


@pytest.mark.parametrize('dtype', [np.float32, np.uint16])
def test_common_reader_accepts_both_alpha_encodings(tmp_path, dtype):
    artifacts = Artifacts(tmp_path, ResizeParams)
    rgb = np.full((2, 3, 3), .25, dtype=np.float32)
    opacity = np.array([[0., .1234567, 1.], [.4, .6, .8]], dtype=np.float32)
    encoded = opacity if dtype == np.float32 else np.rint(opacity * 65535).astype(np.uint16)
    ref = artifacts.image(tmp_path, 'alpha-source', rgb, encoded)
    ctx = SimpleNamespace(check_active=lambda: None)
    with ExitStack() as stack:
        actual_rgb, actual_alpha = open_image(stack, ctx, artifacts, artifacts.contracts[ref.artifact_id])
        expected = encoded if dtype == np.float32 else encoded.astype(np.float32) / 65535
        np.testing.assert_array_equal(actual_rgb, rgb)
        np.testing.assert_array_equal(actual_alpha, expected)


def test_stage_arrays_preserves_sub_uint16_alpha_precision(tmp_path):
    artifacts = Artifacts(tmp_path, ResizeParams)
    rgb = np.full((2, 3, 3), .25, dtype=np.float32)
    alpha = np.array([[0., .1234567, 1.], [.4000001, .6, .8]], dtype=np.float32)
    root = tmp_path / 'scratch'; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 1024**2, min_free_bytes=0) as scratch:
        with scratch.reserve(2 * 3 * 16 + 8192) as reservation:
            _, alpha_ref, outputs, written = stage_arrays(artifacts, reservation, rgb, alpha)
    assert alpha_ref is not None and len(outputs) == 2 and written > 2 * 3 * 16
    assert artifacts.outputs['working_alpha'].dtype == np.float32
    np.testing.assert_array_equal(artifacts.outputs['working_alpha'], alpha)
    assert artifacts.outputs['working_alpha'][0, 1] != np.float32(round(float(alpha[0, 1]) * 65535) / 65535)
