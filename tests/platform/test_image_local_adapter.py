import json
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.algorithms.color import apply_local,apply_series,apply_strength
from photo_worker.contracts.series import ItemCorrection,SeriesCorrections
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.style import LocalColor
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.operations.image.local import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from test_image_color_adapter import Artifacts,context


def setup(root,strength):
    a=Artifacts(root,False)
    a.base_rgb=a.rgb.copy()
    a.rgb=np.clip(a.rgb*.8,0,1)
    processed=a.image.model_copy(update={'pixels':a.array(root,'processed',a.rgb)})
    processed_ref=a.add(processed)
    values=a.resolved.model_dump(exclude={'hash'})
    values.update(strength=strength,local=LocalColor(enabled=True,mask_policy_id="person_guided_v1",person_ev=.2,background_ev=-.2,person_saturation=1.1,background_saturation=.9))
    a.resolved=ResolvedStyle.create(**values)
    a.person=np.full(a.rgb.shape[:2],.7,np.float32)
    mask=MaskRef(artifact=a.array(root,'person',a.person),canvas=a.image.canvas,transform_chain=a.image.transform_chain,
                 selector='person',confidence=1.,model_lock_id=None,protected=False)
    a.raw=dict(image=processed_ref.model_dump(mode='json'),base_aligned=a.image_ref.model_dump(mode='json'),
               resolved=a.add(a.resolved).model_dump(mode='json'),person=mask.model_dump(mode='json'))
    return a

@pytest.mark.parametrize('strength',[0.,.3,1.])
def test_local_and_strength_once(tmp_path,strength):
    a=setup(tmp_path,strength);ctx=context(a);scratch_root=tmp_path/'scratch';scratch_root.mkdir()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    expected=apply_strength(a.base_rgb,apply_local(a.rgb,a.person,a.resolved.local),strength)
    assert np.array_equal(a.output,expected)
    assert a.staged[0][1].alpha==a.image.alpha
    report=a.staged[1][1]
    assert report.local_applied and report.local_skip is None and report.strength_applied
    assert len(result.outputs)==3 and not result.review_required


def test_unreliable_mask_skips_local_but_completes_strength(tmp_path):
    a=setup(tmp_path,.4);a.raw['person']['confidence']=.5
    scratch_root=tmp_path/'scratch';scratch_root.mkdir()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(context(a),a)
    assert np.array_equal(a.output,apply_strength(a.base_rgb,a.rgb,.4))
    assert result.review_required and a.staged[1][1].local_skip=='mask_unreliable'


def test_series_correction_applies_before_local_and_strength(tmp_path):
    a=setup(tmp_path,.4);item_id=uuid4()
    correction=ItemCorrection(item_id=item_id,raw_ev=.2,raw_gains=[1.02,.98,1.],applied_ev=.2,
        applied_gains=[1.02,.98,1.],confidence=1,accepted=True,reason='applied')
    reference_id=uuid4()
    reference=ItemCorrection(item_id=reference_id,raw_ev=0,raw_gains=[1.,1.,1.],applied_ev=0,
        applied_gains=[1.,1.,1.],confidence=1,accepted=True,reason='reference')
    series=SeriesCorrections(scene_id='all',reference_item_id=reference_id,common_region_ids=['neutral'],items=[correction,reference],warnings=[])
    series_ref=a.add(series);a.raw['item_id']=str(item_id);a.raw['series']=series_ref.model_dump(mode='json')
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        a.scratch=scratch;execute(context(a),a)
    balanced=apply_series(a.rgb,correction.applied_ev,correction.applied_gains)
    expected=apply_strength(a.base_rgb,apply_local(balanced,a.person,a.resolved.local),.4)
    assert np.array_equal(a.output,expected)
    report=a.staged[1][1]
    assert report.series==series_ref
    assert report.series_applied and report.series_reason=='applied'


def test_mask_canvas_mismatch_rejected_before_upload(tmp_path):
    a=setup(tmp_path,1.);a.raw['person']['canvas']['space_id']='other'
    with pytest.raises(OperationFailure) as e:execute(context(a),a)
    assert e.value.error.code=='INPUT_INVALID' and not a.uploads


def test_regions_apply_and_missing_region_is_reported(tmp_path):
    from photo_worker.algorithms.regions import apply_regions
    from photo_worker.contracts.style import RegionAdjustment
    a=setup(tmp_path,.6)
    r=RegionAdjustment(region_id='coat',selector='clothing',ev=.3,saturation=1.2,hue_deg=10.,strength=.7,priority=1)
    missing=r.model_copy(update={'region_id':'sky','selector':'sky'})
    body=a.resolved.model_dump(exclude={'hash'})
    body['modules']['extensions']['region_adjustments']=[r,missing]
    a.resolved=ResolvedStyle.create(**body)
    a.raw['resolved']=a.add(a.resolved).model_dump(mode='json')
    mask=dict(a.raw['person']);mask.update(selector='clothing')
    a.raw['regions']=[dict(region_id='coat',mask=mask)]
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(context(a),a)
    local=apply_local(a.rgb,a.person,a.resolved.local)
    expected=apply_strength(a.base_rgb,apply_regions(local,[(r,a.person)]).rgb,.6)
    assert np.array_equal(a.output,expected) and result.review_required
    report=a.staged[1][1]
    assert [(s.region_id,s.reason) for s in report.region_skips]==[('sky','missing_mask')]
    assert {c.stage for c in report.clipping}=={'local','regions'}


def test_cancelled_local_cleans_scratch(tmp_path):
    a=setup(tmp_path,1.);ctx=context(a);root=tmp_path/'scratch';root.mkdir();calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==9:raise RuntimeError('cancelled')
    ctx.check_active=active
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with pytest.raises(RuntimeError,match='cancelled'):execute(ctx,a)
        assert not list(scratch.directory.iterdir()) and not a.uploads


@pytest.mark.parametrize('selector,invert',[('person',False),('background',True)])
def test_regional_person_and_background_use_exact_probability_complement(tmp_path,selector,invert):
    from photo_worker.contracts.style import RegionAdjustment, Disabled
    from photo_worker.algorithms.regions import apply_regions
    a=setup(tmp_path,1.)
    rule=RegionAdjustment(region_id='r',selector=selector,ev=.4,saturation=1.,hue_deg=0.,strength=1.,priority=0)
    values=a.resolved.model_dump(exclude={'hash'})
    values['local']=Disabled()
    modules=a.resolved.modules.model_copy(update={'extensions':a.resolved.modules.extensions.model_copy(update={'region_adjustments':[rule]})})
    values['modules']=modules
    a.resolved=ResolvedStyle.create(**values)
    a.raw['resolved']=a.add(a.resolved).model_dump(mode='json')
    a.raw['regions']=[{'region_id':'r','mask':a.raw['person'],'invert':invert}]
    ctx=context(a)
    ctx.lease.command.inputs.append(MaskRef.model_validate_json(json.dumps(a.raw['person'])).artifact)
    scratch_root=tmp_path/'scratch';scratch_root.mkdir()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    expected=apply_regions(a.rgb,[(rule,1-a.person if invert else a.person)]).rgb
    assert np.allclose(a.output,expected,rtol=0,atol=1e-7)
    assert a.staged[1][1].region_skips==[] and not result.review_required


def test_region_inversion_refuses_nonbackground_rule(tmp_path):
    from photo_worker.contracts.style import RegionAdjustment
    a=setup(tmp_path,1.)
    values=a.resolved.model_dump(exclude={'hash'})
    values['modules']=a.resolved.modules.model_copy(update={'extensions':a.resolved.modules.extensions.model_copy(update={'region_adjustments':[RegionAdjustment(region_id='r',selector='person',ev=0.,saturation=1.,hue_deg=0.,strength=1.,priority=0)]})})
    a.resolved=ResolvedStyle.create(**values);a.raw['resolved']=a.add(a.resolved).model_dump(mode='json')
    a.raw['regions']=[{'region_id':'r','mask':a.raw['person'],'invert':True}]
    with pytest.raises(OperationFailure,match='INPUT_INVALID'):
        execute(context(a),a)
    assert a.uploads==[]


def test_region_contract_refuses_arbitrary_inversion(tmp_path):
    from photo_worker.contracts.image_local import RegionMask
    from pydantic import ValidationError
    a=setup(tmp_path,1.)
    for change in ({'selector':'manual'},{'protected':True}):
        raw={**a.raw['person'],**change}
        with pytest.raises(ValidationError):RegionMask.model_validate_json(json.dumps({'region_id':'r','mask':raw,'invert':True}))


@pytest.mark.parametrize('selector',['skin','sky','clothing','manual'])
def test_regional_override_binding_is_explicit_and_confirmed(tmp_path,selector):
    from photo_worker.contracts.override import OverrideMaskBinding
    from pydantic import ValidationError
    a=setup(tmp_path,1.)
    mask={**a.raw['person'],'selector':selector}
    raw={'binding_id':'r','purpose':'region_adjustment','mask':mask,'confirmed':True,'strength':1.}
    binding=OverrideMaskBinding.model_validate_json(json.dumps(raw))
    assert binding.mask.selector==selector
    with pytest.raises(ValidationError):OverrideMaskBinding.model_validate_json(json.dumps({**raw,'confirmed':False}))
    with pytest.raises(ValidationError):OverrideMaskBinding.model_validate_json(json.dumps({**raw,'mask':{**mask,'protected':True}}))
