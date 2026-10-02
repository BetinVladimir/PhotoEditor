"""Frozen scalar edits preserve unrelated style settings and enforce bounds."""

import json
from pathlib import Path
from uuid import UUID

import pytest

from photo_worker.contracts.style import StyleV2
from photo_worker.operations.learning.contracts import FrozenRecipe, RenderCase
from photo_worker.operations.learning.recipe import (
    materialize_frozen_style, materialize_holdout_styles,
    materialize_predicted_style)
from test_v2_learning_fit_adapter import frozen_recipe, ref
from test_v2_learning_evaluation import inputs


STYLE = (Path(__file__).resolve().parents[2] /
         'docs/implementation/contracts/examples/style-fixed.example.json')


def fixture():
    base = StyleV2.model_validate(json.loads(STYLE.read_text()))
    image, style_ref = ref(700), ref(701)
    binding = dict(asset_version_id=UUID(int=702), source=image,
                   canvas_hash='a' * 64)
    recipe = FrozenRecipe.model_validate(dict(schema_version=1, source=binding,
        base_style=style_ref, effective_delta=dict(ev=.25, log_r=.01,
        log_g=0, log_b=-.01, saturation_delta=.05, retouch_delta=0),
        renderer_version='v2.2', icc_lock_sha='b' * 64,
        model_lock_sha='c' * 64))
    return base, recipe, style_ref


def test_frozen_style_materializes_without_changing_other_modules():
    base, recipe, style_ref = fixture()
    result = materialize_frozen_style(recipe, base, style_ref, recipe.source,
                                      'b' * 64, 'c' * 64)
    assert result.global_.gains_rgb[0] > base.global_.gains_rgb[0]
    assert result.global_.saturation == 1.05
    assert result.beauty == base.beauty
    assert result.local == base.local and result.quality == base.quality
    assert base.global_.gains_rgb == [1, 1, 1]


def test_frozen_style_rejects_rebinding_and_out_of_range_delta():
    base, recipe, style_ref = fixture()
    with pytest.raises(ValueError, match='FROZEN_RECIPE_BINDING_MISMATCH'):
        materialize_frozen_style(recipe, base, ref(999), recipe.source,
                                 'b' * 64, 'c' * 64)
    base.global_.gains_rgb[:] = [4, 4, 4]
    with pytest.raises(ValueError, match='PARAMETER_RANGE'):
        materialize_frozen_style(recipe, base, style_ref, recipe.source,
                                 'b' * 64, 'c' * 64)


def test_nonzero_retouch_enables_beauty_only_with_required_models():
    base, recipe, style_ref = fixture()
    recipe.effective_delta.retouch_delta = .05
    with pytest.raises(ValueError, match='Missing model dependencies'):
        materialize_frozen_style(recipe, base, style_ref, recipe.source,
                                 'b' * 64, 'c' * 64)
    base.models = [dict(id='sfd', weight_id='test'),
                   dict(id='fan2d', weight_id='test')]
    # A validated StyleV2 carries the qualified model references.
    base = StyleV2.model_validate(base.model_dump(mode='json', by_alias=True))
    result = materialize_frozen_style(recipe, base, style_ref, recipe.source,
                                      'b' * 64, 'c' * 64)
    assert result.beauty.enabled and result.beauty.smoothing == .05
    assert result.beauty.tone_evenness == 0
    assert result.beauty.texture_keep == 1


def test_prediction_composes_with_frozen_base_without_clamping_style():
    base, recipe, style_ref = fixture()
    _, _, baseline, candidate, _, _ = inputs()
    plain = materialize_frozen_style(recipe, base, style_ref, recipe.source,
                                     'b' * 64, 'c' * 64)
    fitted = materialize_predicted_style(
        recipe, base, style_ref, recipe.source, 'b' * 64, 'c' * 64,
        candidate, (0.0,) * 12)
    assert fitted.global_.gains_rgb[0] > plain.global_.gains_rgb[0]
    assert fitted.global_.saturation == plain.global_.saturation
    assert fitted.local == plain.local and fitted.quality == plain.quality
    neutral = materialize_predicted_style(
        recipe, base, style_ref, recipe.source, 'b' * 64, 'c' * 64,
        baseline, (0.0,) * 12)
    assert neutral == plain
    base.global_.gains_rgb[:] = [4, 4, 4]
    with pytest.raises(ValueError, match='PARAMETER_RANGE'):
        materialize_predicted_style(recipe, base, style_ref, recipe.source,
                                    'b' * 64, 'c' * 64, candidate,
                                    (0.0,) * 12)


def test_holdout_styles_share_frozen_base_and_approved_has_no_prediction():
    base = StyleV2.model_validate(json.loads(STYLE.read_text()))
    _, manifest, baseline, candidate, _, _ = inputs()
    item = next(example for example in manifest.examples
                if example.split == 'holdout')
    base_recipe = frozen_recipe(item, manifest)
    approved_recipe = frozen_recipe(item, manifest).model_copy(deep=True)
    approved_recipe.effective_delta.ev = .2
    result = materialize_holdout_styles(item, manifest, base_recipe,
                                        approved_recipe, base,
                                        baseline, candidate)
    assert result.baseline.global_.gains_rgb[0] < result.candidate.global_.gains_rgb[0]
    assert result.baseline.global_.gains_rgb[0] < result.approved.global_.gains_rgb[0]
    expected = materialize_frozen_style(
        approved_recipe, base, approved_recipe.base_style, item.render_case.binding,
        manifest.icc_lock_sha, manifest.model_lock_sha)
    assert result.approved == expected
    approved_recipe.base_style = ref(999)
    with pytest.raises(ValueError, match='FROZEN_RECIPE_STYLE_MISMATCH'):
        materialize_holdout_styles(item, manifest, base_recipe,
                                   approved_recipe, base,
                                   baseline, candidate)


def test_render_case_requires_paired_identity_evidence_refs():
    _, manifest, _, _, _, _ = inputs()
    raw = manifest.examples[0].render_case.model_dump(mode='python')
    assert 'effective_mask' not in raw and 'consent_snapshot' not in raw
    raw['effective_mask'] = ref(900).model_dump(mode='python')
    with pytest.raises(ValueError, match='INCOMPLETE_HOLDOUT_IDENTITY_EVIDENCE'):
        RenderCase.model_validate(raw)
    raw['consent_snapshot'] = ref(901).model_dump(mode='python')
    result = RenderCase.model_validate(raw)
    assert result.effective_mask == ref(900)
    assert result.consent_snapshot == ref(901)
