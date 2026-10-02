"""V23 training selection keeps whole sessions and capture lineages isolated."""

from dataclasses import replace
from datetime import datetime, timedelta, timezone
from uuid import UUID

import pytest

from photo_worker.operations.learning.freeze import (EligibleExample,
                                                     fit_frozen, freeze_examples)


def corpus():
    base = datetime(2026, 1, 1, tzinfo=timezone.utc)
    rows = []
    for session_index in range(6):
        session = UUID(int=session_index + 1)
        for index in range(10):
            number = session_index * 10 + index + 1
            rows.append(EligibleExample(
                event_id=UUID(int=number), session_id=session,
                source_content_sha=f'{number:064x}',
                source_lineage_sha=f'{number:064x}',
                approved_at=base + timedelta(minutes=number),
                features=(number / 100,) + (0.0,) * 11,
                target=(0.1, 0.0, 0.0, 0.0, 0.0, 0.0),
            ))
    return rows


def test_session_split_is_stable_and_holdout_never_enters_train():
    rows = corpus()
    first = freeze_examples(rows)
    second = freeze_examples(list(reversed(rows)))
    assert first == second
    assert len(first.examples) == 60
    assert len(first.holdout_sessions) == 2
    assert len(first.train_sessions) == 4
    assert {item.source.session_id for item in first.examples
            if item.split == 'holdout'} == set(first.holdout_sessions)
    assert {item.source.session_id for item in first.examples
            if item.split == 'train'} == set(first.train_sessions)
    assert len([item for item in first.examples if item.split == 'holdout']) == 20


def test_latest_approved_lineage_wins_before_split():
    rows = corpus()
    later = replace(rows[0], event_id=UUID(int=1000),
                    approved_at=rows[0].approved_at + timedelta(days=1))
    result = freeze_examples(rows + [later])
    assert len(result.examples) == 60
    assert any(item.source.event_id == later.event_id for item in result.examples)
    assert (rows[0].event_id, 'DUPLICATE_LINEAGE') in {
        (item.event_id, item.reason) for item in result.excluded}


def test_conflicting_lineage_or_content_never_enters_either_split():
    rows = corpus()
    # Same capture lineage appears in another session: both events are excluded.
    cross_session = replace(rows[0], event_id=UUID(int=1001),
                            session_id=rows[10].session_id)
    # Same original content is assigned two lineages: neither assertion is safe.
    conflicting_content = replace(rows[1], event_id=UUID(int=1002),
                                  source_lineage_sha='f' * 64)
    result = freeze_examples(rows + [cross_session, conflicting_content])
    kept_ids = {item.source.event_id for item in result.examples}
    assert not kept_ids.intersection({rows[0].event_id, cross_session.event_id,
                                     rows[1].event_id, conflicting_content.event_id})
    assert len(result.examples) == 58
    assert all(item.reason == 'CONFLICTING_LINEAGE' for item in result.excluded)


def test_minimums_are_checked_after_deduplication_without_rebalancing():
    with pytest.raises(ValueError, match='INSUFFICIENT_DATA'):
        freeze_examples(corpus()[:39])
    rows = corpus()
    # Leave all six sessions represented, but only nine holdout examples for
    # the deterministic hashed ordering. They must not be reshuffled to pass.
    ordered = freeze_examples(rows)
    held = set(ordered.holdout_sessions)
    held_sessions = sorted(held, key=str)
    remove = ([item for item in rows if item.session_id == held_sessions[0]][:6] +
              [item for item in rows if item.session_id == held_sessions[1]][:5])
    remaining = [item for item in rows if item not in remove]
    with pytest.raises(ValueError, match='INSUFFICIENT_DATA'):
        freeze_examples(remaining)


def test_holdout_targets_cannot_influence_fit_or_lambda():
    rows = corpus()
    frozen = freeze_examples(rows)
    held = set(frozen.holdout_sessions)
    modified = [replace(item, target=(-0.1, 0.0, 0.0, 0.0, 0.0, 0.0))
                if item.session_id in held else item for item in rows]
    other = freeze_examples(modified)
    weights = (0.4, 0.1, 0.1, 0.1, 0.2, 0.1)
    scales = (1.0, 0.2, 0.2, 0.2, 0.15, 0.15)
    assert fit_frozen(frozen, weights, scales) == fit_frozen(other, weights, scales)
