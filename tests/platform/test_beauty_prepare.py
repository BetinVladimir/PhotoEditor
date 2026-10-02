from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import WorkingImage,ColorSpace
from photo_worker.operations.portrait.prepare import prepare_beauty,FaceInput
from photo_worker.algorithms.skin_mask import build_skin_mask
from photo_worker.runtime.scratch import ScratchSpace
from test_skin_mask import fixture
from test_beauty import params


def inputs():
    face,roi,parsing=fixture();other=face.model_copy(deep=True);other.object_id=uuid4()
    for p in other.points:p.x+=.03
    image=WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),canvas=face.canvas,
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24'),dtype='float32',alpha=None,transform_chain=face.transform_chain)
    rgb=np.random.default_rng(5).uniform(.25,.7,(256,256,3)).astype(np.float32)
    return rgb,image,[FaceInput(face,parsing),FaceInput(other,parsing)],roi


def test_multiple_faces_global_protection_and_cleanup(tmp_path):
    rgb,image,faces,roi=inputs();outputs=[]
    union=np.maximum(*(build_skin_mask(f.face,roi,f.skin_probability).protected for f in faces))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,20*1024**2,min_free_bytes=0) as scratch:
        for entries in (faces,list(reversed(faces))):
            with prepare_beauty(rgb,image,entries,params(),scratch) as result:
                result.reservation.validate_file(result.image)
                out=np.load(result.image,allow_pickle=False);outputs.append(out)
                assert all(e.applied for e in result.faces)
                assert np.array_equal(out[union==1],rgb[union==1])
                assert len(list(scratch.directory.iterdir()))==1
            assert not list(scratch.directory.iterdir())
    assert np.array_equal(*outputs)


def test_empty_faces_and_consumer_error(tmp_path):
    rgb,image,faces,roi=inputs()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,20*1024**2,min_free_bytes=0) as scratch:
        with prepare_beauty(rgb,image,[],params(),scratch) as result:
            assert np.array_equal(np.load(result.image,allow_pickle=False),rgb) and not result.faces
        with pytest.raises(RuntimeError):
            with prepare_beauty(rgb,image,faces,params(),scratch):raise RuntimeError('consumer')
        assert not list(scratch.directory.iterdir())


def test_cancellation_after_patch_creation_cleans_everything(tmp_path):
    rgb,image,faces,roi=inputs()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,20*1024**2,min_free_bytes=0) as scratch:
        def active():
            if any(p.name.startswith('face-delta-') for p in scratch.directory.iterdir()):raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_beauty(rgb,image,faces,params(),scratch,check_active=active):pass
        assert not list(scratch.directory.iterdir())
