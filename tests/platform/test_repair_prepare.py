from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.defects.prepare import prepare_repair
from photo_worker.contracts.defects import accepted_candidates
from photo_worker.runtime.scratch import ScratchSpace
from test_defect_review import setup
from test_defect_labels import label_map


def fixture(tmp_path):
    a,ref,proposal,review=setup(tmp_path);labels=label_map(proposal)
    rgb=np.full((*labels.shape,3),.5,np.float32);rgb[labels>0]=.1
    protected=np.zeros(labels.shape,np.float32)
    return a,ref,proposal,review,labels,rgb,protected


def test_only_accepted_label_changes_and_cleanup(tmp_path):
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    selected=accepted_candidates(ref,proposal,review,a.image_ref,a.image)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_repair(rgb,labels,protected,proposal,selected,scratch) as files:
            output=np.load(files.image,allow_pickle=False)
            assert np.array_equal(output[labels!=2],rgb[labels!=2])
            assert np.mean(output[labels==2])>.3
            assert files.candidates[0].applied_pixels==9
        assert not list(scratch.directory.iterdir())


def test_protected_and_reversed_selection(tmp_path):
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    protected[labels==1]=1;outputs=[]
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        for selected in (proposal.candidates,list(reversed(proposal.candidates))):
            with prepare_repair(rgb,labels,protected,proposal,selected,scratch) as files:
                out=np.load(files.image,allow_pickle=False);outputs.append(out)
                assert files.candidates[0].reason=='protected'
                assert np.array_equal(out[labels==1],rgb[labels==1])
        assert np.array_equal(*outputs)


def test_consumer_exception_and_cancel_cleanup(tmp_path):
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError):
            with prepare_repair(rgb,labels,protected,proposal,[],scratch):raise RuntimeError('consumer')
        def active():
            if any(scratch.directory.iterdir()):raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_repair(rgb,labels,protected,proposal,[],scratch,check_active=active):pass
        assert not list(scratch.directory.iterdir())


def test_border_candidate_is_reported_unchanged(tmp_path):
    from photo_worker.contracts.defects import candidate_id
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    candidate=proposal.candidates[0];bbox=candidate.bbox.model_copy(update={'y':0})
    changed=candidate.model_copy(update={'bbox':bbox,'id':candidate_id(proposal.source_pixels,proposal.label_map,candidate.label,bbox,candidate.diameter)})
    proposal=proposal.model_copy(update={'candidates':[changed,proposal.candidates[1]]})
    labels=label_map(proposal);rgb=np.full((*labels.shape,3),.5,np.float32);rgb[labels>0]=.1
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_repair(rgb,labels,protected,proposal,[changed],scratch) as files:
            assert files.candidates[0].reason=='insufficient_context'
            assert np.array_equal(np.load(files.image,allow_pickle=False),rgb)
