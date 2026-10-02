"""Deterministic V23 training split over already authorized correction cases.

Database, consent and immutable RenderCase checks happen before this pure
algorithm. It removes capture-lineage conflicts before assigning any session
to train or holdout, so no derivative can cross the split boundary.
"""
from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from hashlib import sha256
from math import ceil, isfinite
from uuid import UUID

from .model import (FEATURE_COUNT, TARGET_COUNT, TARGET_LIMITS, FittedDeltaModel,
                    TrainExample, fit_delta_model)


@dataclass(frozen=True)
class EligibleExample:
    event_id: UUID
    session_id: UUID
    source_content_sha: str
    source_lineage_sha: str
    approved_at: datetime
    features: tuple[float, ...]
    target: tuple[float, ...]


@dataclass(frozen=True)
class FrozenExample:
    source: EligibleExample
    split: str


@dataclass(frozen=True)
class ExcludedExample:
    event_id: UUID
    reason: str


@dataclass(frozen=True)
class FreezeResult:
    examples: tuple[FrozenExample, ...]
    excluded: tuple[ExcludedExample, ...]
    train_sessions: tuple[UUID, ...]
    holdout_sessions: tuple[UUID, ...]


def _valid_sha(value: str) -> bool:
    return len(value) == 64 and all(char in '0123456789abcdef' for char in value)


def _session_order(session_id: UUID) -> tuple[bytes, str]:
    canonical = str(session_id)
    return sha256(('2026:' + canonical).encode('utf-8')).digest(), canonical


def freeze_examples(candidates: list[EligibleExample]) -> FreezeResult:
    """Deduplicate lineages, then split whole sessions with fixed seed 2026.

    Candidate eligibility, approval provenance and consent are deliberately
    external inputs to this stage. Invalid or missing bindings are rejected,
    while conflicting source lineages are explicitly excluded from both sets.
    """
    if not candidates or len(candidates) > 5000:
        raise ValueError('INSUFFICIENT_DATA')
    event_ids: set[UUID] = set()
    sessions_by_lineage: dict[str, set[UUID]] = {}
    lineages_by_content: dict[str, set[str]] = {}
    for item in candidates:
        if (item.event_id.int == 0 or item.session_id.int == 0 or
                item.event_id in event_ids or
                not _valid_sha(item.source_content_sha) or
                not _valid_sha(item.source_lineage_sha) or
                item.approved_at.utcoffset() is None or
                len(item.features) != FEATURE_COUNT or
                len(item.target) != TARGET_COUNT or
                any(not isfinite(value) or abs(value) > 100
                    for value in item.features) or
                any(not isfinite(value) or abs(value) > limit
                    for value, limit in zip(item.target, TARGET_LIMITS, strict=True))):
            raise ValueError('INVALID_TRAINING_EXAMPLE')
        event_ids.add(item.event_id)
        sessions_by_lineage.setdefault(item.source_lineage_sha, set()).add(item.session_id)
        lineages_by_content.setdefault(item.source_content_sha, set()).add(
            item.source_lineage_sha)
    conflicted = {lineage for lineage, sessions in sessions_by_lineage.items()
                  if len(sessions) > 1}
    for lineages in lineages_by_content.values():
        if len(lineages) > 1:
            conflicted.update(lineages)
    latest: dict[str, EligibleExample] = {}
    excluded: list[ExcludedExample] = []
    for item in candidates:
        if item.source_lineage_sha in conflicted:
            excluded.append(ExcludedExample(item.event_id, 'CONFLICTING_LINEAGE'))
            continue
        current = latest.get(item.source_lineage_sha)
        if current is None or (item.approved_at, item.event_id.int) > \
                (current.approved_at, current.event_id.int):
            if current is not None:
                excluded.append(ExcludedExample(current.event_id, 'DUPLICATE_LINEAGE'))
            latest[item.source_lineage_sha] = item
        else:
            excluded.append(ExcludedExample(item.event_id, 'DUPLICATE_LINEAGE'))
    kept = list(latest.values())
    sessions = sorted({item.session_id for item in kept}, key=_session_order)
    if len(kept) < 40 or len(sessions) < 6:
        raise ValueError('INSUFFICIENT_DATA')
    holdout_count = max(2, ceil(0.2 * len(sessions)))
    holdout = tuple(sessions[:holdout_count])
    train = tuple(sessions[holdout_count:])
    holdout_set = set(holdout)
    if len(train) < 4:
        raise ValueError('INSUFFICIENT_DATA')
    train_count = sum(item.session_id not in holdout_set for item in kept)
    if train_count < 20 or len(kept) - train_count < 10:
        raise ValueError('INSUFFICIENT_DATA')
    session_position = {session: index for index, session in enumerate(sessions)}
    kept.sort(key=lambda item: (session_position[item.session_id],
                                item.approved_at, item.event_id.int))
    excluded.sort(key=lambda item: item.event_id.int)
    frozen = tuple(FrozenExample(item, 'holdout' if item.session_id in holdout_set
                                 else 'train') for item in kept)
    return FreezeResult(frozen, tuple(excluded), train, holdout)


def fit_frozen(result: FreezeResult, weights: tuple[float, ...],
               loss_scales: tuple[float, ...]) -> FittedDeltaModel:
    """Pass only the frozen train partition to fitting, never the holdout."""
    train = [TrainExample(item.source.event_id, item.source.session_id,
                          item.source.features, item.source.target)
             for item in result.examples if item.split == 'train'
             and item.source.session_id in result.train_sessions]
    if (len(train) < 20 or
            len(train) != sum(item.split == 'train' for item in result.examples) or
            any(item.source.session_id in result.holdout_sessions
                for item in result.examples if item.split == 'train')):
        raise ValueError('INVALID_TRAINING_SPLIT')
    return fit_delta_model(train, weights, loss_scales)
