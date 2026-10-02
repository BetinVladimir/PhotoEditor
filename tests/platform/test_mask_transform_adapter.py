from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import MaskRef,TransformChain,ResizeTransform,Rect
from photo_worker.operations.mask.transform import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_mask_raster_adapter import MaskArtifacts
from test_image_color_adapter import context


def setup(root,labels):
    a=MaskArtifacts(root);canvas=a.image.canvas
    step=ResizeTransform(kind='resize',from_canvas=canvas,to_canvas=canvas,pixel_centers='half_pixel',invertibility='invertible',valid_region=Rect(x=0,y=0,width=canvas.width,height=canvas.height))
    chain=TransformChain.create([step])
    target=a.image.model_copy(update={'transform_chain':chain})
    values=np.full((canvas.height,canvas.width),7 if labels else .4,np.uint16 if labels else np.float32)
    mask=MaskRef(artifact=a.array(root,'source-mask',values),canvas=canvas,transform_chain=TransformChain.create([]),object_id=uuid4(),selector='manual',confidence=.9,model_lock_id=None,protected=True)
    a.raw=dict(mask=mask.model_dump(mode='json'),target=a.add(target).model_dump(mode='json'),step=step.model_dump(mode='json'),dtype='uint16' if labels else 'float32')
    ctx=context(a);ctx.lease.execution_profile['resource_estimate']['ram_bytes']=1073741824
    return a,ctx,mask,chain

@pytest.mark.parametrize('labels',[False,True])
def test_transform_task_preserves_provenance_and_publishes(tmp_path,labels):
    a,ctx,mask,chain=setup(tmp_path,labels);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    manifest=a.staged[0][1]
    assert manifest.mask.transform_chain==chain
    assert manifest.mask.object_id==mask.object_id and manifest.mask.confidence==.9 and manifest.mask.protected
    assert len(result.outputs)==3 and ctx.snapshot_progress()==dict(done=2,total=2)
    assert np.allclose(a.outputs['transformed_mask'],7 if labels else .4,atol=1e-6)
    assert (manifest.guided_radius is None)==labels


def test_target_chain_mismatch_rejected_before_io(tmp_path):
    a,ctx,mask,chain=setup(tmp_path,False)
    a.raw['target']=a.image_ref.model_dump(mode='json')
    with pytest.raises(OperationFailure) as e:execute(ctx,a)
    assert e.value.error.code=='INPUT_INVALID' and not a.uploads
