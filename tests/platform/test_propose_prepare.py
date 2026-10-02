from uuid import uuid4
import numpy as np
import pytest
from PIL import Image
from photo_worker.operations.defects.propose_prepare import prepare_proposals
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.contracts.images import Canvas
from test_beauty_prepare import inputs
from photo_worker.operations.portrait.prepare import FaceInput


def fixture():
    rgb,image,faces,_=inputs()
    canvas=Canvas(width=512,height=512,orientation=1,space_id=image.canvas.space_id,source_sha256=image.canvas.source_sha256)
    image=image.model_copy(update={'canvas':canvas})
    faces=[FaceInput(f.face.model_copy(update={'canvas':canvas}),np.ones((512,512),np.float32)) for f in faces]
    rgb=np.full((512,512,3),.5,np.float32);rgb[270:272,155:157]=.2
    return rgb,image,faces


def test_global_labels_png_and_permutation(tmp_path):
    rgb,image,faces=fixture();results=[]
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        for entries in (faces,list(reversed(faces))):
            with prepare_proposals(rgb,image,entries,scratch) as files:
                labels=np.load(files.labels,allow_pickle=False);results.append(labels)
                assert files.candidates and sum(f.overlapping_candidates for f in files.faces)>=1
                protected=np.load(files.protection,allow_pickle=False)
                assert not np.any(labels[protected>0])
                with Image.open(files.png) as png:assert np.array_equal(np.asarray(png),labels)
                for c in files.candidates:
                    ys,xs=np.where(labels==c.label)
                    assert (xs.min(),ys.min(),xs.max()-xs.min()+1,ys.max()-ys.min()+1)==(c.bbox.x,c.bbox.y,c.bbox.width,c.bbox.height)
            assert not list(scratch.directory.iterdir())
    assert np.array_equal(*results)


def test_cancel_and_empty_faces(tmp_path):
    rgb,image,faces=fixture()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        with prepare_proposals(rgb,image,[],scratch) as files:
            assert not files.candidates and not np.any(np.load(files.labels,allow_pickle=False))
        def active():
            if any(scratch.directory.iterdir()):raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError):
            with prepare_proposals(rgb,image,faces,scratch,check_active=active):pass
        assert not list(scratch.directory.iterdir())
