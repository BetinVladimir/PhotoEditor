from types import SimpleNamespace
from uuid import uuid4, uuid5

import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.series import (ItemCorrection, RegionStats,
    SeriesCorrections, SeriesParams, SeriesStats)
from photo_worker.operations.learning.pipeline_admission import validate_holdout_pipeline
from photo_worker.operations.learning.series_evidence import FrozenSeriesHoldoutEvidence
from photo_worker.operations.learning.series_replay import (
    _frozen_series_correction, _same_series_styles)


def _ref(char):
    return ArtifactRef(artifact_id=uuid4(), sha256=char * 64)


def _styles(*, changed=False):
    series = SimpleNamespace(enabled=True, algorithm='seriesmatch_v1',
        reference='median', ev_limit=.35, rgb_gain_limits=[.95, 1.05])
    styles = [SimpleNamespace(
        extensions=SimpleNamespace(series=series, culling=None,
            composition=None, portrait_detail=None, inpainting=None,
            generation=None, restoration=None, background=None),
        defects=SimpleNamespace(enabled=False),
        geometry=SimpleNamespace(enabled=False), strength=.7) for _ in range(3)]
    if changed:
        styles[1].extensions = SimpleNamespace(**{
            **vars(styles[1].extensions), 'series': SimpleNamespace(
                **{**vars(series), 'ev_limit': .2})})
    return SimpleNamespace(baseline=styles[0], candidate=styles[1],
                           approved=styles[2])


def test_series_pipeline_requires_one_exact_apply_style_and_evidence():
    styles = _styles()
    assert _same_series_styles(styles) is styles.baseline.extensions.series
    validate_holdout_pipeline(styles, reviewed_series=True)
    with pytest.raises(ValueError, match='HOLDOUT_SERIES_EVIDENCE_REQUIRED'):
        validate_holdout_pipeline(styles)
    with pytest.raises(ValueError, match='HOLDOUT_SERIES_STYLE_MISMATCH'):
        validate_holdout_pipeline(_styles(changed=True), reviewed_series=True)


def test_series_replay_selects_exact_item_from_pinned_aggregate():
    tenant, event, run, version = uuid4(), uuid4(), uuid4(), uuid4()
    selected_item, reference_item = uuid4(), uuid4()
    source, stage, params_ref, series_ref = (
        _ref('a'), _ref('b'), _ref('c'), _ref('d'))
    evidence_ref = ArtifactRef(artifact_id=uuid5(run, f'v23-series/{event}'),
                               sha256='e' * 64)
    evidence = FrozenSeriesHoldoutEvidence(schema_version=1,
        tenant_id=tenant, event_id=event, asset_version_id=version,
        apply_run_id=uuid4(), item_id=selected_item,
        producer_task_id=uuid4(), source_image=source, stage_image=stage,
        params=params_ref, series=series_ref)
    stats_refs = [_ref('f'), _ref('1')]
    params = SeriesParams(scene_id='studio', stats=stats_refs)
    region = RegionStats(region_id='sky', mean_rgb=[.5, .5, .5],
        mean_y=.5, coverage=.4, confidence=.9)
    stats = [SeriesStats(item_id=selected_item, image=stage,
        regions=[region], clipping_fraction=.01, warnings=[]),
        SeriesStats(item_id=reference_item, image=_ref('2'),
        regions=[region], clipping_fraction=.01, warnings=[])]
    identity = [1., 1., 1.]
    corrections = SeriesCorrections(scene_id='studio',
        reference_item_id=reference_item, common_region_ids=['sky'],
        items=[ItemCorrection(item_id=selected_item, raw_ev=.1,
            raw_gains=identity, applied_ev=.1, applied_gains=identity,
            confidence=.9, accepted=True, reason='applied'),
            ItemCorrection(item_id=reference_item, raw_ev=0,
                raw_gains=identity, applied_ev=0, applied_gains=identity,
                confidence=1, accepted=True, reason='reference')], warnings=[])
    class Stage:
        canvas = 'same canvas'
        transform_chain = 'same chain'
        color = 'same color'
        dtype = 'float32'
        def model_copy(self, deep=False):
            return self
    stage_image = Stage()
    case = SimpleNamespace(series_evidence=evidence_ref,
        defect_evidence=None, dust_evidence=None, detail_evidence=None,
        binding=SimpleNamespace(asset_version_id=version),
        working_image=source)
    item = SimpleNamespace(event_id=event)
    workspace = SimpleNamespace(source=SimpleNamespace(image=stage_image))
    declared = {evidence_ref.artifact_id: 'FrozenSeriesHoldoutEvidence',
        source.artifact_id: 'WorkingImage',
        stage.artifact_id: 'WorkingImage',
        params_ref.artifact_id: 'SeriesParams',
        series_ref.artifact_id: 'SeriesCorrections',
        **{ref.artifact_id: 'SeriesStats' for ref in stats_refs}}

    class Artifacts:
        def require_declared_contract(self, ref, contract):
            if declared.get(ref.artifact_id) != contract:
                raise ValueError('TASK_INPUT_CONTRACT_MISMATCH')
        def contract(self, ref, model):
            values = {evidence_ref.artifact_id: evidence,
                stage.artifact_id: stage_image,
                params_ref.artifact_id: params,
                series_ref.artifact_id: corrections,
                stats_refs[0].artifact_id: stats[0],
                stats_refs[1].artifact_id: stats[1]}
            return values[ref.artifact_id]

    chosen = _frozen_series_correction(Artifacts(), case, item,
        workspace, _styles(), run, tenant)
    assert chosen.item_id == selected_item and chosen.applied_ev == .1
    declared[stats_refs[0].artifact_id] = 'WorkingImage'
    with pytest.raises(ValueError, match='TASK_INPUT_CONTRACT_MISMATCH'):
        _frozen_series_correction(Artifacts(), case, item, workspace,
                                  _styles(), run, tenant)
    declared[stats_refs[0].artifact_id] = 'SeriesStats'
    stats[0] = stats[0].model_copy(update={'image': source})
    with pytest.raises(ValueError, match='HOLDOUT_SERIES_MATERIAL_MISMATCH'):
        _frozen_series_correction(Artifacts(), case, item, workspace,
                                  _styles(), run, tenant)
