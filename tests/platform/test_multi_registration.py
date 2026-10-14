"""Authored V29 registration unit coverage; no runtime qualification claim."""
import numpy as np
import pytest
from photo_worker.algorithms.multi_registration import (
    RegisteredPair, assemble_registrations, register_pairs,
)


def pair(i, j, x, count=20):
    return RegisteredPair(i, j, np.array([[1., 0, x], [0, 1, 0], [0, 0, 1]]), count, 30)


def test_tree_composes_all_frames_to_selected_reference():
    transforms, review = assemble_registrations(
        [pair(0, 1, 10), pair(1, 2, 20)], [(100, 100)]*3, 1)
    assert not review
    np.testing.assert_array_equal(transforms[0], pair(0, 1, 10).matrix)
    np.testing.assert_array_equal(transforms[1], np.eye(3))
    np.testing.assert_array_equal(transforms[2], pair(0, 1, -20).matrix)
    assert all(not h.flags.writeable for h in transforms)


def test_maximum_tree_ties_are_lexicographic_and_bad_cycle_requires_review():
    edges = [pair(1, 2, 90), pair(0, 2, 30), pair(0, 1, 10)]
    first, review = assemble_registrations(edges, [(100, 100)]*3, 0)
    second, _ = assemble_registrations(list(reversed(edges)), [(100, 100)]*3, 0)
    assert review
    for a, b in zip(first, second):
        np.testing.assert_array_equal(a, b)
    np.testing.assert_array_equal(first[2], pair(0, 1, -30).matrix)


def test_no_missing_frame_or_indirect_focus_registration():
    with pytest.raises(ValueError, match='UNREGISTERED_FRAME'):
        assemble_registrations([pair(0, 1, 10)], [(100, 100)]*3, 0)
    with pytest.raises(ValueError, match='UNREGISTERED_FRAME'):
        assemble_registrations([pair(0, 1, 10), pair(1, 2, 20)], [(100, 100)]*3, 0, direct=True)


def test_duplicate_and_horizon_crossing_pair_rejected():
    with pytest.raises(ValueError, match='REGISTRATION_PAIR_INVALID'):
        assemble_registrations([pair(0, 1, 10)]*2, [(100, 100)]*3, 0)
    bad = RegisteredPair(0, 1, np.array([[1., 0, 0], [0, 1, 0], [-.02, 0, 1]]), 20, 30)
    with pytest.raises(ValueError, match='UNSAFE_REGISTRATION'):
        assemble_registrations([bad], [(100, 100)]*2, 0)


def test_cancellation_propagates_before_graph_assembly():
    def cancel():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        assemble_registrations([pair(0, 1, 0)], [(100, 100)]*2, 0, check_active=cancel)


def test_blank_pairs_do_not_invent_registration():
    proxy = np.zeros((64, 64), dtype=np.uint8)
    pairs = register_pairs([proxy, proxy], [(64, 64)]*2,
                           registration_error_px=1., match_inlier_min=.8)
    assert pairs == ()
    with pytest.raises(ValueError, match='UNREGISTERED_FRAME'):
        assemble_registrations(pairs, [(64, 64)]*2, 0)
