from uuid import uuid4
from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_beauty import RenderBeautyStage
from photo_worker.v2.render_defects import RenderDefectStage
from photo_worker.v2.render_portrait import RenderPortraitBase
from photo_worker.v2.render_effects import effect_review_required,unpack_render_base
from photo_worker.operations.portrait.prepare import FaceEvidence
from photo_worker.operations.defects.prepare import RepairEvidence


def test_skipped_stage_cannot_be_laundered_as_declared_pass():
    pixels=AdjustmentPixels(np.zeros((1,1,3),np.float32),None)
    face=FaceEvidence(uuid4(),False,'low_confidence')
    repair=RepairEvidence('a'*64,1,0,'protected',1)
    for result in (RenderBeautyStage(pixels,(face,),False),RenderPortraitBase(pixels,(face,),False),
            RenderDefectStage(pixels,(repair,),False),RenderBeautyStage(pixels,(),False),RenderDefectStage(pixels,(),False)):
        actual,evidence=unpack_render_base(result)
        assert actual is pixels and evidence is result and effect_review_required(evidence)
    zero=FaceEvidence(uuid4(),False,'zero_strength')
    assert not effect_review_required(RenderBeautyStage(pixels,(zero,),False))
    assert not effect_review_required(RenderPortraitBase(pixels,(),False))
    assert unpack_render_base(pixels)==(pixels,None)


@pytest.mark.parametrize('evidence',[SimpleNamespace(review_required=False),RenderPortraitBase(None,[],False),
    RenderPortraitBase(None,(),0),RenderDefectStage(None,(object(),),False)])
def test_untyped_effect_evidence_is_rejected(evidence):
    with pytest.raises(ValueError,match='EVIDENCE_INVALID'):
        effect_review_required(evidence)


def test_ordered_chain_retains_every_stage_and_does_not_short_circuit_validation():
    from photo_worker.v2.render_effects import RenderEffectChain
    pixels=AdjustmentPixels(np.zeros((1,1,3),np.float32),None)
    beauty=RenderBeautyStage(pixels,(FaceEvidence(uuid4(),True,None),),False)
    repair=RenderDefectStage(pixels,(RepairEvidence('a'*64,1,0,'protected',1),),False)
    chain=RenderEffectChain(pixels,(beauty,repair))
    actual,evidence=unpack_render_base(chain)
    assert actual is pixels and evidence.effects==(beauty,repair) and effect_review_required(chain)
    malformed=RenderPortraitBase(pixels,[],False)
    with pytest.raises(ValueError,match='EVIDENCE_INVALID'):
        effect_review_required(RenderEffectChain(pixels,(repair,malformed)))
    for stages in ((),[beauty],(chain,),(beauty,)*33):
        with pytest.raises(ValueError,match='EVIDENCE_INVALID'):
            unpack_render_base(RenderEffectChain(pixels,stages))


@pytest.mark.parametrize('change',[{'applied':1},{'texture_energy_ratio':float('nan')},
    {'below_zero':float('inf')},{'above_one':1.1},{'reason':'low_confidence','applied':True}])
def test_invalid_face_metrics_cannot_reach_publication(change):
    from dataclasses import replace
    pixels=AdjustmentPixels(np.zeros((1,1,3),np.float32),None)
    face=replace(FaceEvidence(uuid4(),True,None),**change)
    with pytest.raises(ValueError,match='EVIDENCE_INVALID'):
        unpack_render_base(RenderBeautyStage(pixels,(face,),False))


@pytest.mark.parametrize('change',[{'applied_pixels':-1},{'applied_pixels':True},{'label':0},
    {'radius':float('nan')},{'below_zero':-.1},{'candidate_id':'not-a-pin'}])
def test_invalid_repair_metrics_cannot_reach_publication(change):
    from dataclasses import replace
    pixels=AdjustmentPixels(np.zeros((1,1,3),np.float32),None)
    item=replace(RepairEvidence('a'*64,1,9,None,1),**change)
    with pytest.raises(ValueError,match='EVIDENCE_INVALID'):
        unpack_render_base(RenderDefectStage(pixels,(item,),False))
