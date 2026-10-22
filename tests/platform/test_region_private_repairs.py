"""Fresh automatic eligibility uses current pixels, without fake artifact refs."""
from dataclasses import replace
from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.contracts.images import Canvas,Rect
from photo_worker.operations.region.private_repairs import private_inventory
from photo_worker.algorithms.conservative import select_defect_regions


def fixture():
    canvas=Canvas(width=64,height=64,orientation=1,space_id='work',source_sha256='a'*64)
    candidate=SimpleNamespace(label=1,bbox=Rect(x=30,y=30,width=2,height=2),diameter=2.,score=.95)
    inventory=private_inventory(canvas,'b'*64,(candidate,))
    rgb=np.full((64,64,3),.5,np.float32);rgb[30:32,30:32]=[.58,.5,.5]
    labels=np.zeros((64,64),np.uint16);labels[30:32,30:32]=1
    protected=np.zeros((64,64),np.float32)
    return inventory,rgb,labels,protected


def test_fresh_private_inventory_has_no_artifact_identity_and_uses_fixed_thresholds():
    inventory,rgb,labels,protected=fixture()
    assert select_defect_regions(inventory,rgb,labels,protected)==inventory.candidates
    assert not hasattr(inventory,'image') and not hasattr(inventory,'source_pixels')
    assert inventory.candidates[0].id.startswith('region-private:')
    # Changed fresh pixels lose eligibility, despite the original high DoG score.
    rgb[30:32,30:32]=.5
    assert select_defect_regions(inventory,rgb,labels,protected)==()


@pytest.mark.parametrize('value',[np.nextafter(np.float32(0),np.float32(1)),.25,1.])
def test_any_positive_permanent_or_feature_protection_preserves_private_spot(value):
    inventory,rgb,labels,protected=fixture();protected[29,29]=value
    assert select_defect_regions(inventory,rgb,labels,protected)==()


def test_dark_permanent_marks_never_become_automatic_repairs():
    inventory,rgb,labels,protected=fixture();rgb[30:32,30:32]=[.4,.3,.3]
    assert select_defect_regions(inventory,rgb,labels,protected)==()


@pytest.mark.parametrize('change',['id','duplicate','bbox','frame','labels'])
def test_private_inventory_and_physical_labels_cannot_be_rebound(change):
    inventory,rgb,labels,protected=fixture();candidate=inventory.candidates[0]
    if change=='id':inventory=replace(inventory,candidates=(replace(candidate,id='region-private:forged'),))
    elif change=='duplicate':inventory=replace(inventory,candidates=(candidate,candidate))
    elif change=='bbox':inventory=replace(inventory,candidates=(replace(candidate,bbox=Rect(x=31,y=30,width=2,height=2)),))
    elif change=='frame':inventory=replace(inventory,frame_sha='c'*64)
    else:labels[0,0]=2
    with pytest.raises(ValueError):select_defect_regions(inventory,rgb,labels,protected)


def test_private_native_no_face_result_preserves_every_fresh_pixel(tmp_path):
    from uuid import uuid4
    from test_image_color_adapter import Artifacts
    from photo_worker.contracts.base import ArtifactRef,canonical_hash
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.learning.contracts import FrozenRecipe
    from photo_worker.operations.region.execution import RegionGeometryStage,RegionExecutionDescriptor
    from photo_worker.operations.region.private_frames import RegionPrivateFrames
    from photo_worker.operations.region.private_repairs import prepare_private_defects
    a=Artifacts(tmp_path,False);image=a.image.model_copy(update={'alpha':None});source=a.add(image)
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    stage=RegionGeometryStage(operation='defects.auto_repair',operation_version=1,task_id=uuid4(),result_id=uuid4(),
        attempt=1,fencing_token=1,params=ref(),source=source,output=ref(),source_canvas=image.canvas,target_canvas=image.canvas)
    execution=RegionExecutionDescriptor(schema_version=1,source=source,source_canvas_hash=canonical_hash(image.canvas),style=ref(),stages=[stage])
    recipe=FrozenRecipe(schema_version=1,source=dict(asset_version_id=uuid4(),source=source,canvas_hash=canonical_hash(image.canvas)),
        base_style=execution.style,effective_delta=dict(ev=.1,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0),
        renderer_version='v2.2',icc_lock_sha='b'*64,model_lock_sha='c'*64)
    fresh=np.clip(a.rgb*.9,0,1).astype(np.float32);owner=(uuid4(),1,2)
    ledger=RegionPrivateFrames(task_id=owner[0],attempt=owner[1],fence=owner[2],recipe=recipe,source=image,
        rgb=fresh,alpha=None,execution=execution,check_active=lambda:None)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),owner[0],owner[1],64*1048576,min_free_bytes=0) as scratch:
        with prepare_private_defects(fresh,None,image,(),(np.zeros(fresh.shape[:2],np.float32),),scratch,
                ledger=ledger,owner=owner,stage=stage,original_source=source,strength=.5,ram_bytes=1<<30,
                check_active=lambda:None) as (output,inventory,evidence):
            assert np.array_equal(output.view(np.uint32),fresh.view(np.uint32))
            assert inventory.candidates==() and evidence==()
        assert not list(scratch.directory.iterdir())
