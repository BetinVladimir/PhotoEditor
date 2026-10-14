"""Person-statistics setup fences inference and partial buffer allocation."""
from contextlib import contextmanager
from types import SimpleNamespace as NS

import numpy as np
import pytest

from photo_worker.operations.learning import person_stats
from photo_worker.runtime.executor import LeaseLost


@pytest.mark.parametrize('failure', ['infer_cancel', 'mask_cancel', 'luma_allocation'])
def test_person_setup_unwinds_engine_and_partial_mappings(monkeypatch, tmp_path, failure):
    events, closed = [], []
    cancelled = False
    acquired = []

    def active():
        if cancelled:
            raise LeaseLost('person lease lost')

    def infer(value):
        nonlocal cancelled
        events.append('infer')
        if failure == 'infer_cancel':
            cancelled = True
        return np.zeros((2, 2), np.float32)

    @contextmanager
    def engine(*args):
        try:
            yield NS(infer=infer)
        finally:
            events.append('engine_closed')

    @contextmanager
    def reserve(size):
        events.append('reserve')
        try:
            yield NS(path=lambda name, suffix: tmp_path / (name + suffix))
        finally:
            events.append('reservation_closed')

    def allocate(*args, **kwargs):
        nonlocal cancelled
        if acquired:
            raise OSError('luma allocation failed')
        result = NS(_mmap=NS(close=lambda: closed.append('mask')))
        acquired.append(result)
        if failure == 'mask_cancel':
            cancelled = True
        return result

    monkeypatch.setattr(person_stats, 'work_to_model', lambda value: (value, None))
    monkeypatch.setattr(person_stats, '_preview', lambda value, size: value)
    monkeypatch.setattr(person_stats, '_probability', lambda *args: (
        np.zeros((2, 2), np.float32), .9))
    monkeypatch.setattr(person_stats.np.lib.format, 'open_memmap', allocate)
    pool = NS(lock=NS(lock_id='a' * 64, models=[NS(id='lraspp_mobilenet_v3_large')]),
              acquire=engine)
    scratch = NS(reserve=reserve)
    with pytest.raises(OSError if failure == 'luma_allocation' else LeaseLost):
        with person_stats.open_holdout_person_stats(
                np.zeros((2, 2, 3), np.float32), None, pool, 'cuda',
                'a' * 64, scratch, active):
            pytest.fail('yielded failed person workspace')
    assert events[:2] == ['infer', 'engine_closed']
    assert closed == ([] if failure == 'infer_cancel' else ['mask'])
    assert ('reserve' in events) == (failure != 'infer_cancel')
    if failure != 'infer_cancel':
        assert events[-1] == 'reservation_closed'


@pytest.mark.parametrize('dtype', [np.uint16, np.float32])
def test_transparent_exclusion_matches_exact_full_frame_result(dtype):
    height, width = 513, 1025
    alpha = np.ones((height, width), dtype=dtype)
    alpha[::3, ::5] = 0
    if dtype == np.float32:
        alpha[1::3, 1::5] = np.nextafter(np.float32(0), np.float32(1))
    actual = np.full((height, width), .625, np.float32)
    expected = actual.copy()
    expected[alpha == 0] = 0
    checks = []
    person_stats._exclude_transparent(actual, alpha, lambda: checks.append(True))
    np.testing.assert_array_equal(actual, expected)
    assert len(checks) == 7  # Six bounded tiles and a final activity check.


def test_transparent_exclusion_stops_before_next_tile():
    target = np.ones((1, 1025), np.float32)
    alpha = np.zeros((1, 1025), np.float32)
    checks = 0
    def active():
        nonlocal checks
        checks += 1
        if checks == 2:
            raise LeaseLost('stop alpha exclusion')
    with pytest.raises(LeaseLost, match='stop alpha exclusion'):
        person_stats._exclude_transparent(target, alpha, active)
    np.testing.assert_array_equal(target[:, :512], 0)
    np.testing.assert_array_equal(target[:, 512:], 1)


@pytest.mark.parametrize('shape', [(0, 1, 3), (1, 0, 3), (200_000_001, 1, 3)])
def test_person_rejects_invalid_canvas_before_model_or_scratch(shape):
    # Zero-stride view exercises the bound without allocating a huge image.
    source = np.lib.stride_tricks.as_strided(np.zeros((1,), np.float32),
                                           shape=shape, strides=(0, 0, 0))
    pool = NS(lock=NS(lock_id='a' * 64, models=[NS(id='lraspp_mobilenet_v3_large')]))
    with pytest.raises(ValueError, match='HOLDOUT_PERSON_INPUT_INVALID'):
        with person_stats.open_holdout_person_stats(
                source, None, pool, 'cuda', 'a' * 64, None, lambda: None):
            pytest.fail('accepted invalid canvas')
