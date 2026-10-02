"""Frozen scalar edits preserve adaptive prototype/local recipe evidence."""
from copy import deepcopy
from photo_worker.contracts.style import Adaptation, Prototype, LUT, LocalColor, ModelReference, StyleV2
from photo_worker.operations.learning.recipe import materialize_frozen_style
from photo_worker.operations.region.adapter import required_region_modules
from test_region_preview_presentation import fixture, ICC_LOCK
from test_style_resolve import profile


def test_frozen_edit_preserves_adaptation_and_local_prototype_without_mutating_base():
    params,_=fixture()
    base=profile()
    local=LocalColor(enabled=True,person_ev=.2,background_ev=-.1,
        person_saturation=1.1,background_saturation=.9,mask_policy_id='person_guided_v1')
    adaptation=Adaptation(feature_version='stats20_v1',mean=[0.]*20,scale=[.05]*20,
        k=1,temperature=1,ood_threshold=1.,coverage_bins=[True]*512,
        prototypes=[Prototype(id='approved-reference',features=[0.]*20,
            absolute_lut=LUT(kind='absolute',size=33,encoding='constant',values=[.4,.5,.6]),local=local)])
    raw=base.model_dump(mode='json',by_alias=True)
    raw.update(mode='adaptive',adaptation=adaptation.model_dump(mode='json'),
        models=[ModelReference(id='lraspp_mobilenet_v3_large',weight_id='test').model_dump(mode='json')])
    base=StyleV2.model_validate(raw)
    before=deepcopy(base.model_dump(mode='json',by_alias=True))
    recipe=params.frozen_recipe.model_copy(deep=True)
    recipe.effective_delta.ev=.25
    recipe.effective_delta.saturation_delta=.05
    result=materialize_frozen_style(recipe,base,recipe.base_style,recipe.source,ICC_LOCK,recipe.model_lock_sha)
    after=result.model_dump(mode='json',by_alias=True)
    assert base.model_dump(mode='json',by_alias=True)==before
    for field in before.keys()-{'global','beauty'}:
        assert after[field]==before[field],field
    assert after['adaptation']==before['adaptation']
    assert {'adaptive','local'} <= required_region_modules(result)
    assert result.global_.gains_rgb==[value*2**.25 for value in base.global_.gains_rgb]
    assert result.global_.saturation==base.global_.saturation+.05
    result.adaptation.prototypes[0].local.person_ev=.3
    assert base.adaptation.prototypes[0].local.person_ev==.2
