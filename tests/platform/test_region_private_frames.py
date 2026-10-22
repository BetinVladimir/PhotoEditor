"""Private frames are measured attempt state, never fake artifact receipts."""
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_region_execution import fixture as execution_fixture
from photo_worker.contracts.images import TransformChain
from photo_worker.operations.learning.contracts import FrozenRecipe
from photo_worker.operations.region.private_frames import RegionPrivateFrames,pixel_digest


def fixture():
    # Keep one coherent source/Params lineage rather than mixing fixture IDs.
    original=execution_fixture();execution=original.model_copy(update={'stages':original.stages[:1]})
    stage=execution.stages[0]
    source=SimpleNamespace(canvas=stage.source_canvas,transform_chain=TransformChain.create([]))
    recipe=FrozenRecipe(schema_version=1,source=dict(asset_version_id=uuid4(),source=execution.source,canvas_hash=execution.source_canvas_hash),
        base_style=execution.style,effective_delta=dict(ev=.2,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0),
        renderer_version='v2.2',icc_lock_sha='b'*64,model_lock_sha='c'*64)
    rgb=np.full((8,10,3),.3,np.float32);owner=(uuid4(),1,7)
    ledger=RegionPrivateFrames(task_id=owner[0],attempt=owner[1],fence=owner[2],recipe=recipe,source=source,
        rgb=rgb,alpha=None,execution=execution,check_active=lambda:None)
    return ledger,owner,recipe,execution,source,rgb


def test_changed_delta_pixels_have_private_lineage_without_rewriting_origin_ref():
    ledger,owner,recipe,execution,source,rgb=fixture();first=ledger.current
    changed=np.clip(rgb*1.1,0,1).astype(np.float32)
    second=ledger.advance(execution.stages[0],changed,None,source,input_frame=first,policy={'effective_delta':recipe.effective_delta.model_dump(mode='json')},owner=owner)
    assert second.parent_sha==first.pixels_sha and second.pixels_sha!=first.pixels_sha
    assert ledger.finish(owner=owner,recipe=recipe,execution=execution) is second
    assert not hasattr(second,'artifact_id') and execution.stages[0].source==execution.source


@pytest.mark.parametrize('change',['owner','parent','stage','mutation','geometry','incomplete'])
def test_private_lineage_cannot_skip_attempt_source_or_stage_authority(change):
    ledger,owner,recipe,execution,source,rgb=fixture();frame=ledger.current;stage=execution.stages[0]
    if change=='owner':owner=(uuid4(),1,7)
    elif change=='parent':frame=None
    elif change=='stage':stage=stage.model_copy(update={'task_id':uuid4()})
    elif change=='mutation':rgb[0,0,0]=.5
    elif change=='geometry':source=SimpleNamespace(canvas=source.canvas.model_copy(update={'width':11}),transform_chain=source.transform_chain)
    if change=='incomplete':
        with pytest.raises(ValueError,match='INCOMPLETE'):ledger.finish(owner=owner,recipe=recipe,execution=execution)
    else:
        with pytest.raises(ValueError):ledger.advance(stage,rgb,None,source,input_frame=frame,policy={},owner=owner)


def test_private_digest_cancels_between_bounded_tiles_and_accepts_scene_range():
    from photo_worker.contracts.images import Canvas
    canvas=Canvas(width=600,height=300,orientation=1,space_id='work',source_sha256='a'*64)
    rgb=np.full((300,600,3),1.5,np.float32);calls=[]
    def active():
        calls.append(1)
        if len(calls)==3:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):pixel_digest(rgb,None,canvas,active)
    assert len(calls)==3


def sequence_fixture():
    from photo_worker.operations.region.private_frames import RegionPrivateSequence
    ledger,owner,recipe,execution,source,rgb=fixture()
    from photo_worker.contracts.base import ArtifactRef
    first=execution.stages[0]
    second=first.model_copy(update={'operation':'image.inpaint','task_id':uuid4(),'result_id':uuid4(),
        'source':first.output,'output':ArtifactRef(artifact_id=uuid4(),sha256='d'*64),
        'params':ArtifactRef(artifact_id=uuid4(),sha256='e'*64)})
    execution=execution.model_copy(update={'stages':[first,second]})
    verified=[(stage,{},source,source,{}) for stage in execution.stages]
    sequence=RegionPrivateSequence(owner=owner,recipe=recipe,source=source,rgb=rgb,alpha=None,
        execution=execution,verified=verified,check_active=lambda:None)
    return sequence,owner,source,rgb


def test_private_sequence_authorizes_fresh_stage_but_never_adopted_source_relabel():
    from photo_worker.operations.region.private_sources import require_stage_source
    sequence,owner,source,original=sequence_fixture();fresh=original* np.float32(1.1)
    sequence.record('image.color',fresh,None)
    token=sequence.input('image.inpaint',fresh,None);source_ref=token.stage.source
    with pytest.raises(ValueError,match='SOURCE_INVALID'):
        require_stage_source('image.inpaint',source_ref,fresh,None,original,None,check_active=lambda:None)
    require_stage_source('image.inpaint',source_ref,fresh,None,original,None,
        private_input=token,check_active=lambda:None)
    assert token.source==sequence.execution.stages[0].output
    sequence.record('image.inpaint',fresh.copy(),None)
    sequence.finish()
    with pytest.raises(ValueError):
        require_stage_source('image.inpaint',source_ref,fresh,None,original,None,
            private_input=token,check_active=lambda:None)


def test_private_native_sequence_cannot_jump_color_or_use_foreign_source():
    from photo_worker.contracts.base import ArtifactRef
    sequence,owner,source,rgb=sequence_fixture()
    with pytest.raises(ValueError,match='ORDER_INVALID'):sequence.input('image.inpaint',rgb,None)
    sequence.record('image.color',rgb,None)
    token=sequence.input('image.inpaint',rgb,None)
    with pytest.raises(ValueError,match='INPUT_INVALID'):
        token.require('image.inpaint',ArtifactRef(artifact_id=uuid4(),sha256='f'*64),rgb,None)


def retouch_sequence_fixture():
    from photo_worker.contracts.style import Beauty,Disabled
    from photo_worker.operations.region.private_frames import RegionPrivateSequence
    prior,owner,source,rgb=sequence_fixture()
    recipe=prior.recipe.model_copy(update={'effective_delta':prior.recipe.effective_delta.model_copy(update={'retouch_delta':.2})})
    beauty=Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=.2,tone_evenness=0.,texture_keep=1.,
        skin_mask='face68_conservative_v1',protect_features=True)
    value=RegionPrivateSequence(owner=owner,recipe=recipe,source=source,rgb=rgb,alpha=None,
        execution=prior.execution,verified=prior.verified,check_active=lambda:None,
        base_style=SimpleNamespace(beauty=Disabled()),effective_style=SimpleNamespace(beauty=beauty))
    return value,source,rgb


def test_new_retouch_phase_is_private_recipe_execution_not_fake_adopted_task():
    sequence,source,rgb=retouch_sequence_fixture();sequence.record('image.color',rgb,None)
    fresh=rgb*np.float32(1.01);sequence.record('portrait.beauty',fresh,None)
    frame=sequence.ledger.current
    assert frame.stage_task_id is None and frame.parent_sha is not None
    assert not hasattr(frame,'artifact_id') and sequence.position==1
    token=sequence.input('image.inpaint',fresh,None);assert token.frame is frame
    sequence.record('image.inpaint',fresh.copy(),None);sequence.finish()


@pytest.mark.parametrize('change',['missing','repeat','before_color'])
def test_new_retouch_phase_cannot_be_skipped_repeated_or_moved(change):
    sequence,source,rgb=retouch_sequence_fixture()
    if change=='before_color':
        with pytest.raises(ValueError,match='RETOUCH_ORDER'):sequence.record('portrait.beauty',rgb,None)
        return
    sequence.record('image.color',rgb,None)
    if change=='repeat':
        sequence.record('portrait.beauty',rgb,None)
        with pytest.raises(ValueError,match='RETOUCH_ORDER'):sequence.record('portrait.beauty',rgb,None)
    else:
        sequence.record('image.inpaint',rgb,None)
        with pytest.raises(ValueError,match='RETOUCH_INCOMPLETE'):sequence.finish()
