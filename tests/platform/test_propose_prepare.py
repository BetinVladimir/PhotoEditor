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


def test_multi_protection_max_preserves_probabilities_and_order(tmp_path):
    rgb,image,_=fixture()
    sources=[np.full((512,512),value,np.float32) for value in (.25,.5,.75)]
    sources[0][0,0]=1
    snapshots=[source.copy() for source in sources]
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        for entries in (sources[1:],sources[1:][::-1]):
            with prepare_proposals(rgb,image,[],scratch,permanent=sources[0],protections=entries) as files:
                actual=np.load(files.protection,allow_pickle=False)
                assert np.array_equal(actual,np.maximum.reduce(sources))
            assert not list(scratch.directory.iterdir())
        invalid=sources[1].copy();invalid[-1,-1]=np.nan
        with pytest.raises(ValueError,match='permanent values'):
            with prepare_proposals(rgb,image,[],scratch,protections=[invalid]):pass
        assert not list(scratch.directory.iterdir())
    assert all(np.array_equal(a,b) for a,b in zip(sources,snapshots))


def test_degenerate_face_is_reported_without_losing_required_protection(tmp_path):
    from photo_worker.operations.defects.protection import verify_proposal_protection
    rgb,image,faces=fixture()
    original=faces[0].face
    degenerate=original.model_copy(update={'points':[point.model_copy(update={'x':.5,'y':.5}) for point in original.points]})
    entry=FaceInput(degenerate,np.ones((512,512),np.float32))
    required=np.zeros((512,512),np.float32);required[41,17]=.75
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        with prepare_proposals(rgb,image,[entry],scratch,protections=[required]) as files:
            labels=np.load(files.labels,allow_pickle=False)
            protection=np.load(files.protection,allow_pickle=False)
            assert not files.candidates and not np.any(labels)
            assert np.array_equal(protection,required)
            assert files.faces[0].reason=='degenerate_landmarks'
            verify_proposal_protection(image,[entry],protection,labels,protections=[required])
        invalid=entry.skin_probability.copy();invalid[-1,-1]=np.nan
        with pytest.raises(ValueError,match='probability mask'):
            with prepare_proposals(rgb,image,[FaceInput(degenerate,invalid)],scratch):pass
        assert not list(scratch.directory.iterdir())


def test_skipped_degenerate_face_preserves_other_face_labels_and_protection(tmp_path):
    rgb,image,faces=fixture()
    original=faces[0].face
    degenerate=original.model_copy(update={'object_id':uuid4(),'points':[p.model_copy(update={'x':.5,'y':.5}) for p in original.points]})
    skipped=FaceInput(degenerate,np.ones((512,512),np.float32))
    snapshots=[]
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        for selected in (faces,[skipped,*faces]):
            with prepare_proposals(rgb,image,selected,scratch) as files:
                snapshots.append((np.load(files.labels,allow_pickle=False),np.load(files.protection,allow_pickle=False),files.candidates))
                if len(selected)>len(faces):
                    evidence=next(face for face in files.faces if face.object_id==degenerate.object_id)
                    assert evidence.reason=='degenerate_landmarks' and evidence.candidates==0
            assert not list(scratch.directory.iterdir())
    assert np.array_equal(snapshots[0][0],snapshots[1][0])
    assert np.array_equal(snapshots[0][1],snapshots[1][1])
    assert snapshots[0][2]==snapshots[1][2]
