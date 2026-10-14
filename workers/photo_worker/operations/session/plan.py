"""Scene grouping and consistency planning from frozen frame descriptors.

Only descriptors and qualified calibration are inputs. No pixels, identities or
mutable project lookups enter this stage; results are deterministic for a pinned
snapshot, policy and descriptor set.
"""
from __future__ import annotations

from bisect import bisect_left, bisect_right
from dataclasses import dataclass
from datetime import datetime, timedelta
from math import isfinite, log, log2
from typing import Mapping, Sequence
from uuid import UUID, uuid5

import numpy as np


EPS = 1e-6
BLOCK = 128
LOG_MIN = log(0.8)
LOG_MAX = log(1.25)


@dataclass(frozen=True)
class Frame:
    ordinal: int
    captured_at: datetime | None
    camera_hash: str | None
    embedding: tuple[float, ...]
    median_luma: float
    neutral_rgb: tuple[float, float, float] | None
    sharpness: float
    clipping_fraction: float


@dataclass(frozen=True)
class ScenePolicy:
    time_gap_seconds: int
    neighbor_count: int
    camera_offsets: Mapping[str, int]


@dataclass(frozen=True)
class SceneCalibration:
    scene_cosine_min: float
    scene_log_luma_max: float
    cluster_diameter_max: float
    sharpness_scale: float
    clipping_penalty: float


@dataclass(frozen=True)
class SceneGroup:
    id: UUID
    ordinals: tuple[int, ...]
    reference_ordinal: int
    confidence: float


@dataclass(frozen=True)
class Correction:
    ordinal: int
    ev: float
    log_r: float
    log_g: float
    log_b: float
    saturation_delta: float
    retouch_delta: float
    disposition: str
    reason: str | None


def _validate(frames: Sequence[Frame], policy: ScenePolicy, calibration: SceneCalibration) -> np.ndarray:
    if not 1 <= len(frames) <= 5000 or not 30 <= policy.time_gap_seconds <= 7200 or not 1 <= policy.neighbor_count <= 64:
        raise ValueError("INVALID_SESSION_INPUT")
    if len(policy.camera_offsets) > 32 or any(
        len(key) != 64 or any(c not in "0123456789abcdef" for c in key)
        or type(seconds) is not int or not -86400 <= seconds <= 86400
        for key, seconds in policy.camera_offsets.items()
    ):
        raise ValueError("INVALID_CAMERA_OFFSETS")
    values = (calibration.scene_cosine_min, calibration.scene_log_luma_max,
              calibration.cluster_diameter_max, calibration.sharpness_scale,
              calibration.clipping_penalty)
    if not all(isfinite(v) for v in values) or not -1 <= values[0] <= 1 or not 0 <= values[1] <= 4 or not 0 <= values[2] <= 2 or not 1e-6 <= values[3] <= 1e6 or not 0 <= values[4] <= 10:
        raise ValueError("INVALID_SESSION_CALIBRATION")
    ordinals = [f.ordinal for f in frames]
    if len(set(ordinals)) != len(frames) or any(o < 0 or o >= 5000 for o in ordinals):
        raise ValueError("INVALID_SESSION_ORDINAL")
    for f in frames:
        if len(f.embedding) != 384 or not 0 <= f.median_luma <= 1 or not 0 <= f.clipping_fraction <= 1 or not isfinite(f.sharpness) or not 0 <= f.sharpness <= 1e6:
            raise ValueError("INVALID_DESCRIPTOR")
        if f.neutral_rgb is not None and (len(f.neutral_rgb) != 3 or any(not isfinite(v) or not 0 <= v <= 1 for v in f.neutral_rgb)):
            raise ValueError("INVALID_NEUTRAL_RGB")
        if f.captured_at is not None and f.captured_at.utcoffset() is None:
            raise ValueError("NAIVE_CAPTURE_TIME")
    embeddings = np.asarray([f.embedding for f in frames], dtype=np.float64)
    norms = np.linalg.norm(embeddings, axis=1)
    if not np.all(np.isfinite(embeddings)) or not np.all(np.isfinite(norms)) or not np.all(np.abs(norms - 1) <= 1e-4):
        raise ValueError("INVALID_SCENE_EMBEDDING")
    return embeddings


def choose_reference(frames: Sequence[Frame], calibration: SceneCalibration,
                     manual_reference: int | None = None) -> int:
    if not frames:
        raise ValueError("EMPTY_SCENE_GROUP")
    members = {f.ordinal for f in frames}
    if manual_reference is not None:
        if manual_reference not in members:
            raise ValueError("REFERENCE_GROUP_CHANGED")
        return manual_reference
    return max(frames, key=lambda f: (
        f.sharpness / (f.sharpness + calibration.sharpness_scale)
        - calibration.clipping_penalty * f.clipping_fraction, -f.ordinal)).ordinal


def group_scenes(frames: Sequence[Frame], snapshot_id: UUID, policy_sha: str,
                 policy: ScenePolicy, calibration: SceneCalibration,
                 manual_references: Mapping[UUID, int] | None = None) -> list[SceneGroup]:
    if snapshot_id.int == 0 or len(policy_sha) != 64 or any(c not in "0123456789abcdef" for c in policy_sha):
        raise ValueError("INVALID_SCENE_BINDING")
    embeddings = _validate(frames, policy, calibration)
    times = [f.captured_at + timedelta(seconds=policy.camera_offsets.get(f.camera_hash, 0))
             if f.captured_at is not None else None for f in frames]
    known = sorted((i for i, t in enumerate(times) if t is not None), key=lambda i: (times[i], frames[i].ordinal))
    known_indexes = np.asarray(known, dtype=np.intp)
    ordinals_array = np.asarray([frame.ordinal for frame in frames], dtype=np.intp)
    known_times = [times[i] for i in known]
    time_gap = timedelta(seconds=policy.time_gap_seconds)
    edges: dict[tuple[int, int], float] = {}
    for i in known:
        # Inclusive window preserves exact time-gap semantics and original block boundaries.
        try:
            first = bisect_left(known_times, times[i] - time_gap)
        except OverflowError:
            first = 0
        try:
            last = bisect_right(known_times, times[i] + time_gap)
        except OverflowError:
            last = len(known)
        candidate_indexes = np.empty(max(0, last - first - 1), dtype=np.intp)
        candidate_cosines = np.empty(len(candidate_indexes), dtype=np.float64)
        filled = 0
        for start in range((first // BLOCK) * BLOCK, last, BLOCK):
            block = known_indexes[max(start, first):min(start + BLOCK, last)]
            candidates = block[block != i]
            if not len(candidates):
                continue
            similarities = embeddings[candidates] @ embeddings[i]
            count = len(candidates)
            candidate_indexes[filled:filled + count] = candidates
            candidate_cosines[filled:filled + count] = np.clip(similarities, -1.0, 1.0)
            filled += count
        # Exact cosine-descending/ordinal-ascending order, no approximate search.
        ranking = np.lexsort((ordinals_array[candidate_indexes], -candidate_cosines))
        for position in ranking[:policy.neighbor_count]:
            j = int(candidate_indexes[position])
            cosine = float(candidate_cosines[position])
            if cosine < calibration.scene_cosine_min:
                continue
            luma_distance = abs(log2((frames[i].median_luma + EPS) / (frames[j].median_luma + EPS)))
            if luma_distance > calibration.scene_log_luma_max:
                continue
            pair = (min(i, j), max(i, j))
            edges[pair] = cosine
    parent = list(range(len(frames)))
    members: dict[int, list[int]] = {i: [i] for i in range(len(frames))}
    minimum: dict[int, float] = {i: 1.0 for i in range(len(frames))}

    def root(i: int) -> int:
        while parent[i] != i:
            i = parent[i]
        return i

    for (a, b), cosine in sorted(edges.items(), key=lambda item: (
        -item[1], min(frames[item[0][0]].ordinal, frames[item[0][1]].ordinal),
        max(frames[item[0][0]].ordinal, frames[item[0][1]].ordinal))):
        ra, rb = root(a), root(b)
        if ra == rb:
            continue
        combined = members[ra] + members[rb]
        dates = [times[i] for i in combined]
        if (max(dates) - min(dates)).total_seconds() > policy.time_gap_seconds:
            continue
        cross_min = 1.0
        allowed = True
        for start in range(0, len(members[ra]), BLOCK):
            left = embeddings[members[ra][start:start + BLOCK]]
            for offset in range(0, len(members[rb]), BLOCK):
                right = embeddings[members[rb][offset:offset + BLOCK]]
                closest = float(np.min(left @ right.T))
                cross_min = min(cross_min, closest)
                if 1 - closest > calibration.cluster_diameter_max:
                    allowed = False
                    break
            if not allowed:
                break
        if not allowed:
            continue
        parent[rb] = ra
        members[ra] = combined
        minimum[ra] = min(minimum[ra], minimum[rb], cross_min, cosine)
        del members[rb], minimum[rb]
    references = manual_references or {}
    groups = []
    for cluster, indexes in members.items():
        ordered = tuple(sorted(frames[i].ordinal for i in indexes))
        identifier = uuid5(snapshot_id, policy_sha + ":" + ",".join(map(str, ordered)))
        selection = choose_reference([frames[i] for i in indexes], calibration, references.get(identifier))
        groups.append(SceneGroup(identifier, ordered, selection,
                                 0.0 if len(indexes) == 1 else max(0.0, min(1.0, (minimum[cluster] + 1) / 2))))
    groups.sort(key=lambda group: group.ordinals[0])
    if set(references) - {group.id for group in groups}:
        raise ValueError("REFERENCE_GROUP_CHANGED")
    return groups


def solve_consistency(frames: Sequence[Frame], reference_ordinal: int,
                      manual_locked: frozenset[int] = frozenset()) -> list[Correction]:
    by_ordinal = {f.ordinal: f for f in frames}
    if (len(by_ordinal) != len(frames) or reference_ordinal not in by_ordinal
            or not manual_locked.issubset(by_ordinal)):
        raise ValueError("INVALID_SCENE_GROUP")
    reference = by_ordinal[reference_ordinal]
    result = []
    for ordinal in sorted(by_ordinal):
        frame = by_ordinal[ordinal]
        zero = (0.0, 0.0, 0.0, 0.0)
        if ordinal in manual_locked:
            values, disposition, reason = zero, "manual_locked", "MANUAL_LOCKED"
        elif ordinal == reference_ordinal:
            values, disposition, reason = zero, "no_change", "REFERENCE"
        elif frame.median_luma <= EPS or reference.median_luma <= EPS:
            values, disposition, reason = zero, "no_change", "INVALID_LUMINANCE"
        else:
            ev = max(-1.0, min(1.0, log2((reference.median_luma + EPS) / (frame.median_luma + EPS))))
            if frame.neutral_rgb is None or reference.neutral_rgb is None:
                gains, reason = (0.0, 0.0, 0.0), "NO_NEUTRAL_WB"
            else:
                u = [log((r + EPS) / (v + EPS)) for r, v in zip(reference.neutral_rgb, frame.neutral_rgb, strict=True)]
                mean = sum(u) / 3
                gains = tuple(max(LOG_MIN, min(LOG_MAX, value - mean)) for value in u)
                reason = None
            values = (ev, *gains)
            disposition = "apply" if any(abs(value) > 1e-12 for value in values) else "no_change"
        result.append(Correction(ordinal, *values, 0.0, 0.0, disposition, reason))
    return result


def validate_scene_groups(groups: Sequence[SceneGroup], frames: Sequence[Frame],
                          snapshot_id: UUID, policy_sha: str) -> None:
    """Reject incomplete, duplicated or rebound plans before staging outputs."""
    expected = {frame.ordinal for frame in frames}
    if not expected or len(expected) != len(frames) or not groups or len(groups) > len(frames):
        raise ValueError("INVALID_SCENE_PLAN")
    observed: set[int] = set()
    for group in groups:
        ordinals = group.ordinals
        if not ordinals or tuple(sorted(set(ordinals))) != ordinals or group.reference_ordinal not in ordinals:
            raise ValueError("INVALID_SCENE_PLAN")
        if any(ordinal not in expected or ordinal in observed for ordinal in ordinals):
            raise ValueError("INVALID_SCENE_PLAN")
        identifier = uuid5(snapshot_id, policy_sha + ":" + ",".join(map(str, ordinals)))
        if group.id != identifier or not isfinite(group.confidence) or not 0 <= group.confidence <= 1:
            raise ValueError("INVALID_SCENE_PLAN")
        observed.update(ordinals)
    if observed != expected:
        raise ValueError("INVALID_SCENE_PLAN")
