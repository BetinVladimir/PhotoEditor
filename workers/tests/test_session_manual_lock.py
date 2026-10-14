import pytest

from photo_worker.operations.session.plan import Frame, solve_consistency


def frame(ordinal, luma):
    return Frame(ordinal=ordinal, captured_at=None, camera_hash=None,
                 embedding=(1.0,) + (0.0,) * 383, median_luma=luma,
                 neutral_rgb=None, sharpness=1.0, clipping_fraction=0.0)


def test_consistency_retains_current_manual_lock():
    corrections = solve_consistency([frame(0, 0.2), frame(1, 0.4)], 0,
                                    frozenset({1}))
    assert corrections[1].disposition == 'manual_locked'
    assert corrections[1].ev == 0.0


def test_consistency_refuses_lock_from_previous_group():
    with pytest.raises(ValueError, match='INVALID_SCENE_GROUP'):
        solve_consistency([frame(0, 0.2), frame(1, 0.4)], 0,
                          frozenset({2}))
