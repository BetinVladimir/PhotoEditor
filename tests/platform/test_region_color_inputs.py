from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.style import RegionAdjustment
from photo_worker.operations.region.color_inputs import local_evidence,prepare_region_color_inputs
from photo_worker.runtime.scratch import ScratchSpace
from test_image_color_adapter import Artifacts


def fixture(tmp_path):
    a=Artifacts(tmp_path,False);values=np.full(a.rgb.shape[:2],.3,np.float32)
    mask=MaskRef(artifact=a.array(tmp_path,'person-region',values),canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,selector='person',confidence=.9,protected=False,model_lock_id=None)
    rule=RegionAdjustment(region_id='background',selector='background',ev=.2,saturation=1,hue_deg=0,strength=.7,priority=1)
    effective=SimpleNamespace(strength=.5,extensions=SimpleNamespace(region_adjustments=[rule],series=None))
    resolved=SimpleNamespace(strength=.5,modules=SimpleNamespace(extensions=effective.extensions))
    params=dict(image=a.image_ref.model_dump(mode='json'),base_aligned=a.image_ref.model_dump(mode='json'),
        resolved=a.image_ref.model_dump(mode='json'),regions=[dict(region_id='background',mask=mask.model_dump(mode='json'),invert=True)])
    verified=[(SimpleNamespace(operation='image.local'),params,a.image,a.image,{'resolved':resolved})]
    a.require_declared_input=lambda ref:None
    return a,effective,verified,values


def test_person_inversion_is_real_owned_mask_and_source_is_preserved(tmp_path):
    a,effective,verified,values=fixture(tmp_path);before=values.copy()
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,20_000_000,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with prepare_region_color_inputs(a,effective,verified,a.image,None) as bound:
            np.testing.assert_array_equal(bound.region_masks['background'],1-values)
            assert bound.series_correction is None and bound.series_item_id is None
            assert not bound.region_masks['background'].flags.writeable
        assert not list(scratch.directory.iterdir())
    np.testing.assert_array_equal(values,before)


@pytest.mark.parametrize('change',['unreliable','selector','missing','duplicate'])
def test_authored_region_mask_policy_cannot_be_silently_dropped(tmp_path,change):
    _,effective,verified,_=fixture(tmp_path)
    if change=='unreliable':verified[0][1]['regions'][0]['mask']['confidence']=.74
    elif change=='selector':verified[0][1]['regions'][0]['mask']['selector']='manual'
    elif change=='missing':verified[0][1]['regions']=[]
    else:verified=verified+verified
    with pytest.raises(ValueError):local_evidence(effective,verified)


def test_series_uses_exact_frozen_item_and_keeps_rejected_identity():
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
    from photo_worker.contracts.series import SeriesCorrections,ItemCorrection
    from photo_worker.operations.region.color_inputs import series_correction
    selected,reference=uuid4(),uuid4()
    identity=dict(raw_ev=0,raw_gains=[1.,1.,1.],applied_ev=0,applied_gains=[1.,1.,1.],confidence=1)
    corrections=SeriesCorrections(scene_id='scene',reference_item_id=reference,
        common_region_ids=['subject'],warnings=['items_skipped'],items=[
            ItemCorrection(item_id=reference,accepted=True,reason='reference',**identity),
            ItemCorrection(item_id=selected,accepted=False,reason='residual_not_improved',**identity)])
    raw=canonical_bytes(corrections)
    params=SimpleNamespace(series=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(corrections)),item_id=selected)
    requested=SimpleNamespace(extensions=SimpleNamespace(series=SimpleNamespace(ev_limit=.35,rgb_gain_limits=[.95,1.05])))
    actual=series_correction(requested,params,lambda ref:raw)
    assert actual.item_id==selected and not actual.accepted
    assert actual.applied_ev==0 and actual.applied_gains==[1.,1.,1.]
    with pytest.raises(ValueError,match='SERIES_ITEM_REQUIRED'):
        series_correction(requested,SimpleNamespace(series=params.series,item_id=uuid4()),lambda ref:raw)
    altered=corrections.model_copy(update={'items':[corrections.items[0],corrections.items[0]]})
    altered_raw=canonical_bytes(altered)
    with pytest.raises(ValueError,match='SERIES_DUPLICATE_ITEM'):
        series_correction(requested,SimpleNamespace(series=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(altered)),item_id=selected),lambda ref:altered_raw)
