from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef,Canvas,ColorSpace,TransformChain
from photo_worker.algorithms.layer_masks import compile_mask,evaluate_mask_tile
from photo_worker.operations.layers.mask_inputs import resolve_mask_inputs


def fixture(tmp_path):
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    image=WorkingImage(pixels=ref(),alpha=None,dtype='float32',canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    mask_ref,pixels=ref(),ref()
    mask=MaskRef(artifact=pixels,canvas=image.canvas,transform_chain=image.transform_chain,selector='manual',confidence=1,model_lock_id=None,protected=False)
    path=tmp_path/'mask.npy';np.save(path,np.array([[.2,.8]],np.float32),allow_pickle=False)
    leaf={'op':'ref','mask':mask_ref.model_dump(mode='json'),'canvas_hash':canonical_hash(image.canvas)}
    plan=compile_mask({'op':'union','left':leaf,'right':{'op':'invert','operand':leaf}},canonical_hash(image.canvas))
    class Artifacts:
        opens=0;closes=0;contracts=0
        def contract(self,reference,model):
            assert reference==mask_ref and model is MaskRef
            self.contracts+=1;return mask
        @contextmanager
        def input(self,reference,**kwargs):
            assert reference==pixels
            self.opens+=1
            try:yield SimpleNamespace(path=path)
            finally:self.closes+=1
    return image,mask,mask_ref,plan,Artifacts(),path


def test_verified_array_reuse_context_cleanup_and_closed_callback(tmp_path):
    image,mask,ref,plan,artifacts,path=fixture(tmp_path)
    with resolve_mask_inputs(plan,artifacts,image) as read:
        result=evaluate_mask_tile(plan,read,x=0,y=0,width=2,height=1,max_tile_bytes=4096)
        np.testing.assert_allclose(result,[[.8,.8]],atol=1e-7,rtol=0)
        assert artifacts.opens==1 and artifacts.contracts==1
        with pytest.raises(ValueError):read(ArtifactRef(artifact_id=uuid4(),sha256='a'*64),0,0,1,1)
    assert artifacts.closes==1
    with pytest.raises(ValueError,match='CLOSED'):read(ref,0,0,1,1)
    np.testing.assert_allclose(result,[[.8,.8]],atol=1e-7,rtol=0)


def test_bad_array_and_canvas_refuse_with_cleanup(tmp_path):
    image,mask,ref,plan,artifacts,path=fixture(tmp_path)
    np.save(path,np.array([[.2,1.2]],np.float32),allow_pickle=False)
    with pytest.raises(ValueError,match='RANGE_INVALID'):
        with resolve_mask_inputs(plan,artifacts,image):pytest.fail('bad mask admitted')
    assert artifacts.opens==artifacts.closes==1
    mask.canvas=mask.canvas.model_copy(update={'space_id':'other'})
    with pytest.raises(ValueError,match='UNSUPPORTED_TRANSFORM'):
        with resolve_mask_inputs(plan,artifacts,image):pytest.fail('geometry substitution admitted')
    assert artifacts.opens==1


def test_consumer_failure_closes_mapped_inputs(tmp_path):
    image,mask,ref,plan,artifacts,path=fixture(tmp_path)
    with pytest.raises(RuntimeError,match='consumer'):
        with resolve_mask_inputs(plan,artifacts,image) as read:
            assert read(ref,0,0,1,1).shape==(1,1)
            raise RuntimeError('consumer')
    assert artifacts.opens==artifacts.closes==1
    with pytest.raises(ValueError,match='CLOSED'):read(ref,0,0,1,1)
