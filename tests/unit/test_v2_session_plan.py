"""Golden Session Brain planning cases from implementation-v2/10."""
from datetime import datetime, timezone, timedelta
from math import isclose
from uuid import uuid4

import pytest

from photo_worker.operations.session.plan import (
    Frame, SceneCalibration, ScenePolicy, choose_reference,
    group_scenes, solve_consistency, validate_scene_groups,
)


def frame(ordinal, x=1.0, y=0.0, captured=None, luma=0.4, neutral=None,
          sharpness=10.0, clipping=0.0):
    return Frame(ordinal, captured, None, (x, y) + (0.0,) * 382,
                 luma, neutral, sharpness, clipping)


CAL = SceneCalibration(0.7, 1.0, 0.25, 10.0, 1.0)
POLICY = ScenePolicy(60, 2, {})


def test_chain_of_similar_frames_does_not_merge_different_scenes():
    now = datetime(2026, 1, 1, tzinfo=timezone.utc)
    frames = [frame(0, 1, 0, now), frame(1, 0.8, 0.6, now + timedelta(seconds=1)),
              frame(2, 0.28, 0.96, now + timedelta(seconds=2)), frame(3)]
    snapshot = uuid4()
    groups = group_scenes(frames, snapshot, "a" * 64, POLICY, CAL)
    validate_scene_groups(groups, frames, snapshot, "a" * 64)
    assert [g.ordinals for g in groups] == [(0, 1), (2,), (3,)]
    assert groups[0].reference_ordinal == 0
    assert groups[0].confidence >= 0.89
    assert groups[1].confidence == 0.0
    with pytest.raises(ValueError, match="INVALID_SCENE_PLAN"):
        validate_scene_groups(groups[:-1], frames, snapshot, "a" * 64)


def test_reference_and_consistency_golden_cases():
    frames = [frame(0, luma=0.4, neutral=(0.4, 0.4, 0.4), sharpness=10),
              frame(1, luma=0.2, neutral=(0.2, 0.2, 0.2), sharpness=30),
              frame(2, luma=0.2, neutral=None, sharpness=5)]
    assert choose_reference(frames, CAL) == 1
    assert choose_reference(frames, CAL, 0) == 0
    with pytest.raises(ValueError, match="REFERENCE_GROUP_CHANGED"):
        choose_reference(frames, CAL, 9)
    corrections = solve_consistency(frames, 0, frozenset({2}))
    assert corrections[0].disposition == "no_change"
    assert isclose(corrections[1].ev, 1.0, abs_tol=1e-5)
    assert corrections[1].log_r == corrections[1].log_g == corrections[1].log_b == 0.0
    assert corrections[1].saturation_delta == corrections[1].retouch_delta == 0.0
    assert corrections[2].disposition == "manual_locked"
    assert corrections[2].ev == 0.0
    missing_neutral = solve_consistency([frames[0], frame(3, luma=0.2)], 0)[1]
    assert missing_neutral.reason == "NO_NEUTRAL_WB"
    assert isclose(missing_neutral.ev, 1.0, abs_tol=1e-5)


def test_5000_unknown_capture_times_remain_deterministic_singletons():
    frames = [frame(i) for i in range(5000)]
    snapshot = uuid4()
    first = group_scenes(frames, snapshot, "b" * 64, POLICY, CAL)
    second = group_scenes(frames, snapshot, "b" * 64, POLICY, CAL)
    assert first == second
    assert len(first) == 5000
    assert all(group.ordinals == (ordinal,) and group.confidence == 0.0
               for ordinal, group in enumerate(first))
    validate_scene_groups(first, frames, snapshot, "b" * 64)
