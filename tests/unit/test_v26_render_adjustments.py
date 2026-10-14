from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_layer_prepare import scratch,COLOR
from photo_worker.v2.render_adjustments import compose_render_adjustments,AdjustmentPixels


def fixture():
    source=np.array([[[-0.,2.,-1.],[1.,1.,1.]]],np.float32)
    layers=tuple(SimpleNamespace(layer=SimpleNamespace(id=uuid4(),enabled=True,opacity=.5,blend='normal',operation='test.adjust')) for _ in range(2))
    masks=SimpleNamespace(layer=lambda id,x,y,w,h:np.ones((h,w),np.float32),protection=lambda x,y,w,h:np.array([[.001,0]],np.float32)[y:y+h,x:x+w])
    return source,layers,masks


def test_ordered_adjustments_protect_exact_bits_and_release_intermediate_files(tmp_path):
    source,layers,masks=fixture();original=source.copy();space=scratch(tmp_path);seen=[]
    @contextmanager
    def proposal(item,current):
        assert not current.rgb.flags.writeable
        seen.append(current.rgb.copy())
        yield AdjustmentPixels(np.full_like(current.rgb,3),None)
    try:
        with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256) as result:
            np.testing.assert_array_equal(result.rgb[:,0].view(np.uint32),original[:,0].view(np.uint32))
            np.testing.assert_allclose(result.rgb[:,1],2.5)
            np.testing.assert_allclose(seen[1][:,1],2)
            assert not result.rgb.flags.writeable
        np.testing.assert_array_equal(source.view(np.uint32),original.view(np.uint32))
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


@pytest.mark.parametrize('geometry_operation',['optical.lens','image.upscale'])
def test_proposal_failure_closes_current_outputs_and_geometry_preflight_skips_io(tmp_path,geometry_operation):
    source,layers,masks=fixture();space=scratch(tmp_path);calls=[]
    @contextmanager
    def proposal(item,current):
        calls.append(item)
        if len(calls)==2:raise RuntimeError('operator failed')
        yield AdjustmentPixels(np.full_like(current.rgb,3),None)
    try:
        with pytest.raises(RuntimeError):
            with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256):pass
        assert not list(tmp_path.rglob('*.npy'))
        layers[0].layer.operation=geometry_operation;calls.clear()
        with pytest.raises(ValueError,match='GEOMETRY_STAGE'):
            with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096):pass
        assert calls==[]
    finally:space.cleanup()


def test_generated_dependency_survives_identical_pixels_and_full_replacement_removes_it(tmp_path):
    source,layers,masks=fixture();space=scratch(tmp_path)
    masks.protection=lambda x,y,w,h:None
    layers[0].layer.opacity=1;layers[1].layer.opacity=1
    count=0
    @contextmanager
    def proposal(item,current):
        nonlocal count
        count+=1
        generated=np.ones(current.rgb.shape[:2],np.float32) if count==1 else None
        if count==2:np.testing.assert_array_equal(current.generated,1)
        yield AdjustmentPixels(current.rgb.copy(),None,generated)
    try:
        with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256) as result:
            np.testing.assert_array_equal(result.rgb.view(np.uint32),source.view(np.uint32))
            np.testing.assert_array_equal(result.generated,0)
            assert not result.generated.flags.writeable
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


def defect_layers():
    source,layers,masks=fixture()
    for item in layers:
        item.layer.operation='defects.repair'
        item.layer.operation_version='1'
        item.runtime_sha256='b'*64
    return source,layers,masks


def test_defect_outcomes_survive_native_composition_and_owner_exit(tmp_path):
    from photo_worker.v2.render_adjustments import RenderLayerProposal
    from photo_worker.v2.render_effects import RenderLayerEffect,RenderCombinedEffects,effect_review_required
    from photo_worker.operations.defects.prepare import RepairEvidence
    source,layers,masks=defect_layers();space=scratch(tmp_path);closed=[]
    @contextmanager
    def proposal(item,current):
        outcome=RenderLayerEffect(item.layer.id,item.layer.operation,item.layer.operation_version,
            item.runtime_sha256,(RepairEvidence('a'*64,1,0,'protected',1),),False)
        try:yield RenderLayerProposal(AdjustmentPixels(np.full_like(current.rgb,3),None),outcome)
        finally:closed.append(item.layer.id)
    try:
        with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256) as result:
            assert closed==[item.layer.id for item in layers]
            assert tuple(effect.layer_id for effect in result.effects)==tuple(item.layer.id for item in layers)
            assert effect_review_required(RenderCombinedEffects(None,result.effects))
            np.testing.assert_allclose(result.rgb[:,1],2.5)
            np.testing.assert_array_equal(result.rgb[:,0].view(np.uint32),source[:,0].view(np.uint32))
            outcomes=result.effects
        assert effect_review_required(RenderCombinedEffects(None,outcomes))
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


@pytest.mark.parametrize('operation',['defects.repair','portrait.detail','image.denoise'])
@pytest.mark.parametrize('mode',['missing','changed'])
def test_defect_layer_cannot_discard_outcomes_or_rebind_callback_metadata(tmp_path,mode,operation):
    from photo_worker.v2.render_adjustments import RenderLayerProposal
    from photo_worker.v2.render_effects import RenderLayerEffect
    from photo_worker.operations.defects.prepare import RepairEvidence
    source,layers,masks=defect_layers();space=scratch(tmp_path);original=layers[0].layer.id
    for item in layers:item.layer.operation=operation
    @contextmanager
    def proposal(item,current):
        pixels=AdjustmentPixels(np.full_like(current.rgb,3),None)
        if mode=='missing':yield pixels
        else:
            item.layer.id=uuid4()
            yield RenderLayerProposal(pixels,RenderLayerEffect(item.layer.id,'defects.repair','1',
                item.runtime_sha256,(RepairEvidence('a'*64,1,9,None,1),),False))
    try:
        with pytest.raises(ValueError,match='EFFECT_REQUIRED|BINDING_CHANGED'):
            with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256):pass
        assert layers[0].layer.id==original
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


def test_provider_exit_cannot_rewrite_retained_region_evidence(tmp_path):
    from photo_worker.contracts.portrait_detail import DetailEvidence
    from photo_worker.v2.render_adjustments import RenderLayerProposal
    from photo_worker.v2.render_effects import RenderLayerEffect,RenderCombinedEffects,effect_review_required
    source,layers,masks=defect_layers();layers=layers[:1];layers[0].layer.operation='portrait.detail'
    region=DetailEvidence(region_id='teeth-1',kind='teeth',applied=False,changed_pixels=0,
        max_ev=0,chroma_reduction=0,reason='zero_strength')
    @contextmanager
    def proposal(item,current):
        effect=RenderLayerEffect(item.layer.id,'portrait.detail','1',item.runtime_sha256,(),False,(region,))
        yield RenderLayerProposal(AdjustmentPixels(current.rgb,None),effect)
        region.reason='review_required'
    space=scratch(tmp_path)
    try:
        with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256) as result:
            assert region.reason=='review_required'
            assert result.effects[0].regions[0].reason=='zero_strength'
            assert not effect_review_required(RenderCombinedEffects(None,result.effects))
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()
