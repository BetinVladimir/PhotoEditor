import pytest
from photo_worker.algorithms.effective import resolve_effective_params
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.base import canonical_bytes
from test_image_color_adapter import Artifacts


def source(tmp_path,strength):
    a=Artifacts(tmp_path,False)
    body=a.resolved.model_dump(exclude={'hash'})
    body['strength']=strength
    body['modules']['beauty']=dict(enabled=True,algorithm='beautyfilter_v1',smoothing=.8,tone_evenness=.4,texture_keep=.9,skin_mask='face68_conservative_v1',protect_features=True)
    body['modules']['geometry']=dict(enabled=True,algorithm='bodywarp_v1',body_strength=.6,face_slim=.04,waist_slim=.08,hip_slim=.06,torso_slim=.05,leg_length=.02,protect_background=True,max_displacement_image_fraction=.02,on_unsafe='skip_module')
    body['modules']['extensions']['generation']=dict(enabled=True,model='sd15_inpaint',prompt='test',negative_prompt='',seed=42,steps=20,guidance_scale=7.,strength=.8,canvas=[512,512],require_acceptance=True)
    return ResolvedStyle.create(**body)

@pytest.mark.parametrize('strength',[0.,.5,1.])
def test_effect_strength_once_and_no_source_mutation(tmp_path,strength):
    original=source(tmp_path,strength);before=canonical_bytes(original)
    effective=resolve_effective_params(original)
    assert effective.modules.beauty.smoothing==.8*strength
    assert effective.modules.beauty.texture_keep==.9
    assert effective.modules.geometry.body_strength==.6*strength
    assert effective.modules.geometry.face_slim==.04*strength
    assert effective.modules.geometry.waist_slim==.08
    assert effective.modules.extensions.generation.strength==.8*strength
    assert effective.color_strength==strength
    assert canonical_bytes(original)==before
    assert effective==resolve_effective_params(original)
    with pytest.raises(TypeError):resolve_effective_params(effective)


def test_mutated_resolved_hash_rejected(tmp_path):
    original=source(tmp_path,.5)
    original.modules.beauty.smoothing=.7
    with pytest.raises(ValueError,match='hash'):resolve_effective_params(original)
