from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import Canvas,Rect,DenseWarpTransform
from photo_worker.algorithms.dense_correspondence import dense_source_point


def test_dense_conventions_agree_without_half_pixel_shift():
    canvas=Canvas(width=4,height=3,orientation=1,space_id='source',source_sha256='a'*64)
    step=DenseWarpTransform(kind='dense_warp',from_canvas=canvas,to_canvas=canvas,
        invertibility='partial',valid_region=Rect(x=0,y=0,width=4,height=3),
        inverse_field=ArtifactRef(artifact_id=uuid4(),sha256='b'*64))
    field=np.stack(np.meshgrid(np.arange(4),np.arange(3)),axis=-1).astype(np.float32)
    marked=step.model_copy(update={'pixel_centers':'half_pixel'})
    for point in [(.5,.5),(3.5,2.5),(1.25,1.75)]:
        assert dense_source_point(step,field,point)==point
        assert dense_source_point(marked,field+.5,point)==point
    assert dense_source_point(marked,field+.5,(-.5,.5)) is None
    bad=field.copy();bad[0,0]=np.nan
    with pytest.raises(ValueError,match='FIELD_INVALID'):dense_source_point(step,bad,(.5,.5))
    shifted=field+20
    assert dense_source_point(step,shifted,(.5,.5)) is None


def test_chain_correspondence_composes_dense_resize_crop_and_matrix():
    from photo_worker.contracts.images import TransformChain,CropTransform,ResizeTransform,MatrixTransform
    from photo_worker.algorithms.dense_correspondence import chain_source_point
    def canvas(w,h,name):return Canvas(width=w,height=h,orientation=1,space_id=name,source_sha256='a'*64)
    source=canvas(8,6,'source');cropped=canvas(4,3,'crop');resized=canvas(8,6,'resize');target=canvas(8,6,'target')
    def common(before,after):return dict(from_canvas=before,to_canvas=after,invertibility='partial',valid_region=Rect(x=0,y=0,width=after.width,height=after.height))
    crop=CropTransform(kind='crop',crop_rect=Rect(x=2,y=1,width=4,height=3),**common(source,cropped))
    resize=ResizeTransform(kind='resize',pixel_centers='half_pixel',**common(cropped,resized))
    ref=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    dense=DenseWarpTransform(kind='dense_warp',pixel_centers='half_pixel',inverse_field=ref,**common(resized,target))
    chain=TransformChain.create([crop,resize,dense])
    field=np.stack(np.meshgrid(np.arange(8)+.5,np.arange(6)+.5),axis=-1).astype(np.float32)
    assert chain_source_point(chain,(3.,2.),lambda requested:field)==(3.5,2.)
    assert chain_source_point(chain,(.5,.5),lambda requested:field)==(2.5,1.5)
    matrix=MatrixTransform(kind='matrix',forward_matrix=[1.,0.,1.,0.,1.,0.,0.,0.,1.],**common(resized,target))
    chain=TransformChain.create([crop,resize,matrix])
    assert chain_source_point(chain,(4.,2.),lambda requested:pytest.fail('no dense field'))==(3.5,2.)
    assert chain_source_point(chain,(.5,.5),lambda requested:field) is None
