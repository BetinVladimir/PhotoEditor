from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import Canvas,ColorSpace,DenseWarpTransform,Rect,TransformChain,WorkingImage
from photo_worker.runtime.correspondence import open_source_correspondence


def test_runtime_correspondence_maps_verified_field_and_closes(tmp_path):
    canvas=Canvas(width=4,height=3,orientation=1,space_id='source',source_sha256='a'*64)
    ref=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    step=DenseWarpTransform(kind='dense_warp',from_canvas=canvas,to_canvas=canvas,
        inverse_field=ref,pixel_centers='half_pixel',invertibility='partial',valid_region=Rect(x=0,y=0,width=4,height=3))
    image=WorkingImage(pixels=ref,alpha=None,dtype='float32',canvas=canvas,
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([step,step]))
    path=tmp_path/'field.npy'
    field=np.stack(np.meshgrid(np.arange(4)+.5,np.arange(3)+.5),axis=-1).astype(np.float32)
    np.save(path,field,allow_pickle=False)
    class Artifacts:
        opened=0;closed=0
        @contextmanager
        def input(self,requested,suffix):
            assert requested==ref;self.opened+=1
            try:yield NS(path=path)
            finally:self.closed+=1
    artifacts=Artifacts()
    with open_source_correspondence(artifacts,image) as resolve:
        assert resolve((1.25,1.75))==(1.25,1.75)
        assert resolve((0.,.5)) is None
        assert artifacts.opened==1
    assert artifacts.closed==1
    with pytest.raises(RuntimeError,match='CLOSED'):resolve((1.,1.))
