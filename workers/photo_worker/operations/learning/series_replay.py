"""Replay one SQL-authorized Apply series correction on all holdout arms."""
from __future__ import annotations

from contextlib import contextmanager
from uuid import uuid5

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.series import (ItemCorrection, SeriesCorrections,
                                           SeriesParams, SeriesStats)
from photo_worker.operations.series.balance import compute_series_corrections

from .series_evidence import FrozenSeriesHoldoutEvidence


def _same_series_styles(styles):
    selected = [style.extensions.series for style in
                (styles.baseline, styles.candidate, styles.approved)]
    if (any(value is None or not value.enabled for value in selected) or
            any(value != selected[0] for value in selected[1:]) or
            any(style.strength != styles.baseline.strength for style in
                (styles.candidate, styles.approved)) or
            selected[0].algorithm != 'seriesmatch_v1' or
            selected[0].reference != 'median' or
            selected[0].ev_limit != .35 or
            selected[0].rgb_gain_limits != [.95, 1.05]):
        raise ValueError('HOLDOUT_SERIES_STYLE_MISMATCH')
    return selected[0]


def _frozen_series_correction(artifacts, case, item, workspace, styles,
                              training_run_id, tenant_id, check_active=lambda: None):
    check_active()
    style = _same_series_styles(styles)
    if case.series_evidence is None:
        raise ValueError('HOLDOUT_SERIES_EVIDENCE_REQUIRED')
    artifacts.require_declared_contract(case.series_evidence,
                                        'FrozenSeriesHoldoutEvidence')
    evidence = artifacts.contract(case.series_evidence,
                                  FrozenSeriesHoldoutEvidence).model_copy(deep=True)
    if (case.series_evidence.artifact_id != uuid5(
            training_run_id, f'v23-series/{item.event_id}') or
            evidence.tenant_id != tenant_id or
            evidence.event_id != item.event_id or
            evidence.asset_version_id != case.binding.asset_version_id or
            evidence.source_image != case.working_image or
            any(value is not None for value in (
                case.defect_evidence, case.dust_evidence,
                case.detail_evidence))):
        raise ValueError('HOLDOUT_SERIES_BINDING_MISMATCH')
    for ref, contract in (
            (evidence.source_image, 'WorkingImage'),
            (evidence.stage_image, 'WorkingImage'),
            (evidence.params, 'SeriesParams'),
            (evidence.series, 'SeriesCorrections')):
        artifacts.require_declared_contract(ref, contract)
    stage = artifacts.contract(evidence.stage_image, WorkingImage).model_copy(deep=True)
    source = workspace.source.image
    if (stage.canvas != source.canvas or
            stage.transform_chain != source.transform_chain or
            stage.color != source.color or stage.dtype != 'float32'):
        raise ValueError('HOLDOUT_SERIES_SOURCE_CHANGED')
    params = artifacts.contract(evidence.params, SeriesParams).model_copy(deep=True)
    series = artifacts.contract(evidence.series, SeriesCorrections).model_copy(deep=True)
    if (params.scene_id != series.scene_id or
            params.algorithm != style.algorithm or
            series.algorithm != style.algorithm or
            params.reference_item_id is not None or
            params.ev_limit != style.ev_limit or
            params.gain_min != style.rgb_gain_limits[0] or
            params.gain_max != style.rgb_gain_limits[1] or
            params.min_confidence != .75 or
            len(params.stats) != len(series.items) or
            len({ref.artifact_id for ref in params.stats}) != len(params.stats)):
        raise ValueError('HOLDOUT_SERIES_MATERIAL_MISMATCH')
    items = {value.item_id: value for value in series.items}
    if (len(items) != len(series.items) or
            evidence.item_id not in items or
            series.reference_item_id not in items):
        raise ValueError('HOLDOUT_SERIES_MATERIAL_MISMATCH')
    stats = []
    for ref in params.stats:
        check_active()
        artifacts.require_declared_contract(ref, 'SeriesStats')
        stats.append(artifacts.contract(ref, SeriesStats).model_copy(deep=True))
    check_active()
    by_item = {value.item_id: value for value in stats}
    if (len(by_item) != len(stats) or set(by_item) != set(items) or
            by_item[evidence.item_id].image != evidence.stage_image or
            any(value.algorithm != style.algorithm for value in stats)):
        raise ValueError('HOLDOUT_SERIES_MATERIAL_MISMATCH')
    common = sorted(set.intersection(*(
        {region.region_id for region in value.regions} for value in stats)))
    if common != series.common_region_ids:
        raise ValueError('HOLDOUT_SERIES_MATERIAL_MISMATCH')
    # Source provenance pins the result bytes; replay the exact producer
    # algorithm so those bytes also have to follow from the sealed Stats.
    expected = compute_series_corrections(params, stats, check_active)
    if canonical_bytes(expected) != canonical_bytes(series):
        raise ValueError('HOLDOUT_SERIES_CORRECTION_MISMATCH')
    selected = items[evidence.item_id]
    return ItemCorrection.model_validate_json(canonical_bytes(selected))


@contextmanager
def open_holdout_series(artifacts, item, workspace, styles, *,
                        training_run_id, tenant_id,
                        check_active=lambda: None):
    """Keep one immutable selected correction for the three color renders."""
    check_active()
    correction = _frozen_series_correction(artifacts, item.render_case,
        item, workspace, styles, training_run_id, tenant_id, check_active)
    frozen = canonical_bytes(correction)
    check_active()
    yield correction
    if canonical_bytes(correction) != frozen:
        raise ValueError('HOLDOUT_SERIES_CORRECTION_CHANGED')
    check_active()
