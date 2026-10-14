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


def test_5000_known_sparse_times_have_no_false_temporal_neighbors():
    now = datetime(2026, 1, 1, tzinfo=timezone.utc)
    frames = [frame(i, captured=now + timedelta(seconds=61*i)) for i in range(5000)]
    snapshot = uuid4()
    groups = group_scenes(frames, snapshot, "c"*64, POLICY, CAL)
    assert len(groups) == 5000
    assert all(g.ordinals == (i,) for i, g in enumerate(groups))
    validate_scene_groups(groups, frames, snapshot, "c"*64)


def test_time_window_includes_exact_gap_and_excludes_one_microsecond_beyond():
    now = datetime(2026, 1, 1, tzinfo=timezone.utc)
    frames = [frame(0, captured=now), frame(1, captured=now+timedelta(seconds=60)),
              frame(2, captured=now-timedelta(seconds=60, microseconds=1))]
    groups = group_scenes(frames, uuid4(), "d"*64, POLICY, CAL)
    assert [g.ordinals for g in groups] == [(0, 1), (2,)]


@pytest.mark.parametrize("captured", [datetime.min.replace(tzinfo=timezone.utc), datetime.max.replace(tzinfo=timezone.utc)])
def test_time_window_preserves_valid_datetime_extremes(captured):
    groups = group_scenes([frame(0, captured=captured)], uuid4(), "e"*64, POLICY, CAL)
    assert groups[0].ordinals == (0,)


def test_equal_timestamp_burst_is_permutation_invariant_across_block_boundary():
    now = datetime(2026, 1, 1, tzinfo=timezone.utc)
    frames = [frame(i, captured=now) for i in range(129)]
    snapshot = uuid4()
    first = group_scenes(frames, snapshot, 'f'*64, ScenePolicy(60, 64, {}), CAL)
    reversed_plan = group_scenes(list(reversed(frames)), snapshot, 'f'*64,
                                 ScenePolicy(60, 64, {}), CAL)
    assert first == reversed_plan
    assert len(first) == 1
    assert first[0].ordinals == tuple(range(129))
    assert first[0].reference_ordinal == 0
    validate_scene_groups(first, frames, snapshot, 'f'*64)


@pytest.mark.parametrize('seed', range(8))
def test_mixed_scene_topk_preserves_preoptimization_golden_plan(seed):
    import json
    from pathlib import Path
    import numpy as np
    baseline = json.loads((Path(__file__).resolve().parents[2] /
        'docs/implementation-v2/audit/v22-topk-baseline.json').read_text())
    expected = baseline['cases'][seed]['groups']
    rng = np.random.default_rng(seed)
    now = datetime(2026, 1, 1, tzinfo=timezone.utc)
    frames = []
    for i in range(257):
        embedding = rng.normal(0, .008, 384)
        embedding[i % 3] += 1
        embedding /= np.linalg.norm(embedding)
        frames.append(Frame(i, now + timedelta(seconds=i % 71), None,
                            tuple(embedding), .3 + float(rng.random()) * .1,
                            None, float(rng.random()) * 20, 0))
    from uuid import UUID
    groups = group_scenes(frames, UUID('00000000-0000-4000-8000-000000000001'),
                         'a'*64, ScenePolicy(60, 64, {}), CAL)
    assert len(groups) == len(expected)
    for actual, golden in zip(groups, expected, strict=True):
        assert str(actual.id) == golden['id']
        assert actual.ordinals == tuple(golden['ordinals'])
        assert actual.reference_ordinal == golden['reference']
        assert actual.confidence == pytest.approx(golden['confidence'], abs=1e-12, rel=0)
