import numpy as np
import pytest
from uuid import uuid4

from photo_worker.algorithms.color import (
    apply_global, apply_local, apply_series, apply_strength, identity_lut, trilinear,
)
from photo_worker.algorithms.statistics import coverage_counts_bounded
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.style import (
    Adaptation, LocalColor, LUT, ModelReference, Prototype, RegionAdjustment, Series,
)
from photo_worker.contracts.series import ItemCorrection
from photo_worker.operations.learning.color_render import (
    render_materialized_style, render_resolved_color,
)

from test_image_color_adapter import Artifacts
from test_style_resolve import profile


def _resolved(original, **changes):
    body = original.model_dump(exclude={'hash'})
    body.update(changes)
    return ResolvedStyle.create(**body)


@pytest.mark.parametrize('adaptive', [False, True])
def test_holdout_color_uses_v2_global_and_single_strength_blend(tmp_path, adaptive):
    fixture = Artifacts(tmp_path, adaptive)
    resolved = _resolved(fixture.resolved, strength=.4)
    target = np.empty_like(fixture.rgb)
    render_resolved_color(fixture.rgb, target, resolved,
                          absolute_lut=fixture.grid, tile=256)
    colored = (np.clip(trilinear(fixture.grid, fixture.rgb), 0, 1).astype(np.float32)
               if adaptive else apply_global(fixture.rgb, fixture.config))
    expected = apply_strength(fixture.rgb, colored, .4)
    np.testing.assert_array_equal(target, expected)


def test_holdout_color_uses_v2_local_and_region_order(tmp_path):
    fixture = Artifacts(tmp_path, False)
    local = LocalColor(enabled=True, mask_policy_id='person_guided_v1',
                       person_ev=.2, background_ev=-.2,
                       person_saturation=1.1, background_saturation=.9)
    rule = RegionAdjustment(region_id='sky', selector='sky', ev=.1,
                            saturation=1.05, hue_deg=2, strength=.8, priority=1)
    modules = fixture.resolved.modules.model_copy(deep=True)
    modules.extensions.region_adjustments = [rule]
    resolved = _resolved(fixture.resolved, strength=.5, local=local,
                         modules=modules)
    person = np.full(fixture.rgb.shape[:2], .7, np.float32)
    region = np.full(fixture.rgb.shape[:2], .3, np.float32)
    target = np.empty_like(fixture.rgb)
    render_resolved_color(fixture.rgb, target, resolved,
                          person_mask=person, region_masks={'sky': region},
                          tile=256)
    from photo_worker.algorithms.regions import apply_regions
    colored = apply_global(fixture.rgb, fixture.config)
    expected = apply_strength(fixture.rgb, apply_regions(
        apply_local(colored, person, local), [(rule, region)]).rgb, .5)
    np.testing.assert_array_equal(target, expected)


def test_holdout_color_refuses_missing_masks_and_unsupported_effects(tmp_path):
    fixture = Artifacts(tmp_path, False)
    target = np.empty_like(fixture.rgb)
    local = LocalColor(enabled=True, mask_policy_id='person_guided_v1',
                       person_ev=.1, background_ev=0,
                       person_saturation=1, background_saturation=1)
    resolved = _resolved(fixture.resolved, local=local)
    with pytest.raises(ValueError, match='HOLDOUT_PERSON_MASK_MISSING'):
        render_resolved_color(fixture.rgb, target, resolved)
    modules = fixture.resolved.modules.model_copy(deep=True)
    modules.extensions.region_adjustments = [RegionAdjustment(
        region_id='sky', selector='sky', ev=.1, saturation=1,
        hue_deg=0, strength=1, priority=1)]
    resolved = _resolved(fixture.resolved, modules=modules)
    with pytest.raises(ValueError, match='HOLDOUT_REGION_MASK_MISSING'):
        render_resolved_color(fixture.rgb, target, resolved)


def test_holdout_series_uses_pinned_item_and_production_stage_order(tmp_path):
    fixture = Artifacts(tmp_path, False)
    style = profile().model_copy(deep=True)
    style.extensions.series = Series(
        enabled=True, algorithm='seriesmatch_v1', reference='studio',
        ev_limit=.35, rgb_gain_limits=[.95, 1.05])
    item_id = uuid4()
    correction = ItemCorrection(
        item_id=item_id, raw_ev=.1, raw_gains=[1.01, 1., 1.],
        applied_ev=.1, applied_gains=[1.01, 1., 1.], confidence=1.,
        accepted=True, reason='applied')
    target = np.empty_like(fixture.rgb)
    render_materialized_style(
        fixture.rgb, target, style, person_reliable=False,
        series_correction=correction, series_item_id=item_id, tile=256)
    global_rgb = apply_global(fixture.rgb, style.global_)
    series_rgb = apply_series(global_rgb, correction.applied_ev,
                              correction.applied_gains)
    expected = apply_strength(fixture.rgb,
                              apply_local(series_rgb, None, style.local),
                              style.strength)
    np.testing.assert_array_equal(target, expected)
    with pytest.raises(ValueError, match='HOLDOUT_SERIES_EVIDENCE_REQUIRED'):
        render_materialized_style(
            fixture.rgb, target, style, person_reliable=False,
            series_correction=correction, series_item_id=uuid4(), tile=256)
    with pytest.raises(ValueError, match='HOLDOUT_SERIES_EVIDENCE_REQUIRED'):
        render_materialized_style(
            fixture.rgb, target, style, person_reliable=False, tile=256)


def test_materialized_fixed_and_adaptive_styles_use_pinned_resolution(tmp_path):
    fixture = Artifacts(tmp_path, False)
    source = fixture.rgb
    target = np.empty_like(source)
    fixed = profile()
    result = render_materialized_style(
        source, target, fixed, person_reliable=False, tile=256)
    assert result.evidence['mode'] == 'fixed'
    np.testing.assert_array_equal(
        target, apply_strength(source, apply_global(source, fixed.global_),
                               fixed.strength))

    lut = LUT(kind='absolute', size=33, encoding='dense',
              values=identity_lut(33).reshape(-1).tolist())
    adaptation = Adaptation(feature_version='stats20_v1', mean=[0.] * 20,
        scale=[1.] * 20, k=1, temperature=1, ood_threshold=1,
        prototypes=[Prototype(id='one', features=[0.] * 20,
                              absolute_lut=lut, local=fixed.local)],
        coverage_bins=[True] * 512)
    adaptive = fixed.model_copy(update={
        'mode': 'adaptive', 'adaptation': adaptation,
        'models': [ModelReference(id='lraspp_mobilenet_v3_large',
                                  weight_id='test')]})
    counts = coverage_counts_bounded(source, encoding='gamma24')
    result = render_materialized_style(
        source, target, adaptive, person_reliable=False,
        features=np.zeros(20), coverage_counts=counts, tile=256)
    assert result.evidence['mode'] == 'adaptive'
    np.testing.assert_array_equal(
        target, apply_strength(source, trilinear(result.absolute_lut, source),
                               adaptive.strength))
    with pytest.raises(ValueError, match='ADAPTIVE_FEATURES_REQUIRED'):
        render_materialized_style(source, target, adaptive,
                                  person_reliable=False)
