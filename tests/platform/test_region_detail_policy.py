from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.style import PortraitDetail
from photo_worker.operations.region.detail import detail_entry


def policy(enabled=True):
    detail=PortraitDetail(enabled=True,teeth_strength=.2,shine_strength=.1,under_eye_strength=.3,glasses_policy='review') if enabled else None
    return SimpleNamespace(strength=.5,extensions=SimpleNamespace(portrait_detail=detail))


def entry():
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64).model_dump(mode='json')
    return (SimpleNamespace(operation='portrait.detail'),dict(image=ref,resolved=ref,regions=[]),None,None,{})


def test_enabled_detail_needs_exactly_one_authenticated_stage():
    with pytest.raises(ValueError,match='COVERAGE_MISMATCH'):detail_entry(policy(),[])
    with pytest.raises(ValueError,match='COVERAGE_MISMATCH'):detail_entry(policy(),[entry(),entry()])
    with pytest.raises(ValueError,match='COVERAGE_MISMATCH'):detail_entry(policy(False),[entry()])
    assert detail_entry(policy(False),[]) is None


def test_stage_params_alone_do_not_authorize_effective_detail_policy():
    with pytest.raises(ValueError,match='EFFECTIVE_POLICY_MISMATCH'):
        detail_entry(policy(),[entry()])


def test_detail_reuses_original_strength_scaling_exactly_once(tmp_path):
    from test_image_color_adapter import Artifacts
    from photo_worker.contracts.resolved import ResolvedStyle
    a=Artifacts(tmp_path,False);effective=policy()
    values=a.resolved.model_dump(exclude={'hash'})
    values['strength']=effective.strength
    values['modules']['extensions']['portrait_detail']=effective.extensions.portrait_detail
    resolved=ResolvedStyle.create(**values)
    stage,params,_,_,_=entry()
    params.update(image=a.image_ref.model_dump(mode='json'))
    actual=(stage,params,a.image,a.image,{'resolved':resolved})
    replay=detail_entry(effective,[actual])
    assert replay[-1].teeth_strength==.1
    assert replay[-1].shine_strength==.05
    assert replay[-1].under_eye_strength==.15
    assert resolved.modules.extensions.portrait_detail.teeth_strength==.2


def test_native_detail_preserves_protected_pixels_and_reuses_real_algorithm(tmp_path):
    import numpy as np
    from test_image_color_adapter import Artifacts
    from photo_worker.contracts.images import MaskRef
    from photo_worker.contracts.resolved import ResolvedStyle
    from photo_worker.contracts.portrait_detail import DetailRegion
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.region.detail import prepare_region_detail
    from photo_worker.operations.portrait.prepare_detail import prepare_detail
    a=Artifacts(tmp_path,False);effective=policy()
    values=a.resolved.model_dump(exclude={'hash'})
    values['strength']=effective.strength;values['modules']['extensions']['portrait_detail']=effective.extensions.portrait_detail
    resolved=ResolvedStyle.create(**values)
    target=np.ones(a.rgb.shape[:2],np.float32);protection=np.zeros_like(target);protection[:10]=1
    mask=MaskRef(artifact=a.array(tmp_path,'detail-target',target),canvas=a.image.canvas,transform_chain=a.image.transform_chain,selector='teeth',confidence=1,protected=False,model_lock_id=None)
    protected=MaskRef(artifact=a.array(tmp_path,'detail-protected',protection),canvas=a.image.canvas,transform_chain=a.image.transform_chain,selector='manual',confidence=1,protected=True,model_lock_id=None)
    region=DetailRegion(region_id='teeth',kind='teeth',mask=mask,confirmed=True,strength=.7)
    stage=SimpleNamespace(operation='portrait.detail')
    params=dict(image=a.image_ref.model_dump(mode='json'),resolved=a.add(resolved).model_dump(mode='json'),
        regions=[region.model_dump(mode='json')],protected=[protected.model_dump(mode='json')])
    verified=[(stage,params,a.image,a.image,{'resolved':resolved})]
    a.require_declared_input=lambda ref:None
    root=tmp_path/'scratch';root.mkdir()
    detail=detail_entry(effective,verified)[-1]
    with prepare_detail(a.rgb,[region],detail,lambda ref:target,protection) as native:
        expected=native.rgb.copy()
    with ScratchSpace(root,uuid4(),uuid4(),1,20_000_000,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with prepare_region_detail(a,effective,verified,a.rgb,None,a.image) as rendered:
            np.testing.assert_array_equal(rendered.view(np.uint32),expected.view(np.uint32))
            np.testing.assert_array_equal(rendered[:10].view(np.uint32),a.rgb[:10].view(np.uint32))
        assert not list(scratch.directory.iterdir())
