"""Materialize the six frozen scalar edits without changing other StyleV2 fields."""
from __future__ import annotations

from dataclasses import dataclass
from math import exp, isfinite

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.session.contracts import VersionBinding

from .contracts import (CandidateProfile, FrozenRecipe, RenderCase,
                        TrainingExample, TrainingManifest)
from .model import FittedDeltaModel, predict_delta


def validate_frozen_render_case(case: RenderCase, baseline: FrozenRecipe,
                                approved: FrozenRecipe,
                                manifest: TrainingManifest) -> None:
    """Reject incompatible frozen recipes before fit or holdout rendering."""
    if case.binding.source != case.working_image:
        raise ValueError('FROZEN_RECIPE_BINDING_MISMATCH')
    for recipe in (baseline, approved):
        if (recipe.source != case.binding or
                recipe.renderer_version != manifest.renderer_version or
                recipe.icc_lock_sha != manifest.icc_lock_sha or
                recipe.model_lock_sha != manifest.model_lock_sha):
            raise ValueError('FROZEN_RECIPE_BINDING_MISMATCH')
    if baseline.base_style != approved.base_style:
        raise ValueError('FROZEN_RECIPE_STYLE_MISMATCH')


def materialize_frozen_style(recipe: FrozenRecipe, base: StyleV2,
                             base_ref: ArtifactRef, binding: VersionBinding,
                             icc_lock_sha: str,
                             model_lock_sha: str,
                             predicted_delta: tuple[float, ...] | None = None) -> StyleV2:
    """Return a fully validated copy; out-of-range edits fail, never clamp."""
    if (recipe.source != binding or recipe.base_style != base_ref or
            recipe.icc_lock_sha != icc_lock_sha or
            recipe.model_lock_sha != model_lock_sha or
            base.renderer_version != 'photo_renderer_v2'):
        raise ValueError('FROZEN_RECIPE_BINDING_MISMATCH')
    data = base.model_dump(mode='json', by_alias=True)
    if predicted_delta is None:
        predicted_delta = (0.0,) * 6
    if len(predicted_delta) != 6 or any(not isfinite(value)
                                        for value in predicted_delta):
        raise ValueError('INVALID_PREDICTED_DELTA')
    delta = recipe.effective_delta
    ev = delta.ev + predicted_delta[0]
    logs = (delta.log_r + predicted_delta[1],
            delta.log_g + predicted_delta[2],
            delta.log_b + predicted_delta[3])
    try:
        gains = [gain * 2 ** ev * exp(log_gain)
                 for gain, log_gain in zip(data['global']['gains_rgb'],
                                           logs, strict=True)]
    except OverflowError as exc:
        raise ValueError('PARAMETER_RANGE') from exc
    saturation = (data['global']['saturation'] + delta.saturation_delta +
                  predicted_delta[4])
    previous = data['beauty']['smoothing'] if data['beauty']['enabled'] else 0.0
    smoothing = previous + delta.retouch_delta + predicted_delta[5]
    if (not all(isfinite(value) and .25 <= value <= 4 for value in gains) or
            not isfinite(saturation) or not 0 <= saturation <= 2 or
            not isfinite(smoothing) or not 0 <= smoothing <= 1):
        raise ValueError('PARAMETER_RANGE')
    data['global']['gains_rgb'] = gains
    data['global']['saturation'] = saturation
    if data['beauty']['enabled']:
        data['beauty']['smoothing'] = smoothing
    elif smoothing > 0:
        data['beauty'] = {
            'enabled': True, 'algorithm': 'beautyfilter_v1',
            'smoothing': smoothing, 'tone_evenness': 0.0,
            'texture_keep': 1.0, 'skin_mask': 'face68_conservative_v1',
            'protect_features': True,
        }
    return StyleV2.model_validate(data)


def materialize_predicted_style(recipe: FrozenRecipe, base: StyleV2,
                                base_ref: ArtifactRef, binding: VersionBinding,
                                icc_lock_sha: str, model_lock_sha: str,
                                profile: CandidateProfile,
                                features: tuple[float, ...]) -> StyleV2:
    """Apply clamped inference to the frozen base recipe, then validate style.

    The inference clamp is the only clamp; an out-of-range final StyleV2 fails.
    """
    model = FittedDeltaModel(tuple(profile.means), tuple(profile.scales),
                             tuple(tuple(row) for row in profile.coefficients),
                             tuple(profile.intercept), profile.ridge_lambda)
    predicted = predict_delta(model, features)
    return materialize_frozen_style(recipe, base, base_ref, binding,
                                    icc_lock_sha, model_lock_sha, predicted)


@dataclass(frozen=True)
class FrozenHoldoutStyles:
    baseline: StyleV2
    candidate: StyleV2
    approved: StyleV2


def materialize_holdout_styles(item: TrainingExample,
                               manifest: TrainingManifest,
                               base_recipe: FrozenRecipe,
                               approved_recipe: FrozenRecipe,
                               base_style: StyleV2,
                               baseline_profile: CandidateProfile,
                               candidate_profile: CandidateProfile) -> FrozenHoldoutStyles:
    """Freeze all three renders against one base style and source binding.

    The two model predictions compose with the baseline recipe; the human
    approved recipe is rendered as recorded, without a learning prediction.
    """
    case = item.render_case
    validate_frozen_render_case(case, base_recipe, approved_recipe, manifest)
    if (baseline_profile.profile_id != manifest.profile_id or
            candidate_profile.profile_id != manifest.profile_id or
            baseline_profile.feature_order != 'scene12_v1' or
            candidate_profile.feature_order != 'scene12_v1' or
            len(item.features) != 12 or
            any(not isfinite(value) for value in item.features)):
        raise ValueError('INVALID_HOLDOUT_STYLE_INPUT')
    common = (base_style, base_recipe.base_style, case.binding,
              manifest.icc_lock_sha, manifest.model_lock_sha)
    features = tuple(item.features)
    return FrozenHoldoutStyles(
        baseline=materialize_predicted_style(base_recipe, *common,
                                             baseline_profile, features),
        candidate=materialize_predicted_style(base_recipe, *common,
                                              candidate_profile, features),
        approved=materialize_frozen_style(approved_recipe, *common),
    )
