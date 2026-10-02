from uuid import uuid4
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.defects import DefectCandidate,DefectCandidates,DefectReview,candidate_id,accepted_candidates
from photo_worker.contracts.images import Rect
from test_image_color_adapter import Artifacts


def setup(root):
    a=Artifacts(root,False);labels=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    candidates=[]
    for label in (1,2):
        bbox=Rect(x=10*label,y=5,width=3,height=3);diameter=3.
        candidates.append(DefectCandidate(id=candidate_id(a.image.pixels,labels,label,bbox,diameter),label=label,bbox=bbox,diameter=diameter,score=.9))
    proposal=DefectCandidates(image=a.image_ref,source_pixels=a.image.pixels,label_map=labels,canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,candidates=candidates)
    ref=a.add(proposal)
    review=DefectReview(proposal=ref,image=a.image_ref,label_map=labels,revision=1,
        accepted_candidate_ids=[candidates[1].id],rejected_candidate_ids=[])
    return a,ref,proposal,review


def test_only_explicit_acceptance_and_stable_selection(tmp_path):
    a,ref,proposal,review=setup(tmp_path)
    result=accepted_candidates(ref,proposal,review,a.image_ref,a.image)
    assert [c.label for c in result]==[2]
    review.accepted_candidate_ids=[]
    assert accepted_candidates(ref,proposal,review,a.image_ref,a.image)==()


def test_unknown_stale_image_and_mask_rejected(tmp_path):
    a,ref,proposal,review=setup(tmp_path)
    review.accepted_candidate_ids=['a'*64]
    with pytest.raises(ValueError,match='UNKNOWN'):accepted_candidates(ref,proposal,review,a.image_ref,a.image)
    review.accepted_candidate_ids=[proposal.candidates[0].id]
    image=a.image.model_copy(update={'pixels':ArtifactRef(artifact_id=uuid4(),sha256='c'*64)})
    with pytest.raises(ValueError,match='SOURCE'):accepted_candidates(ref,proposal,review,a.image_ref,image)
    review.label_map=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    with pytest.raises(ValueError,match='REVIEW'):accepted_candidates(ref,proposal,review,a.image_ref,a.image)


def test_conflicting_decisions_and_tampered_candidate(tmp_path):
    a,ref,proposal,review=setup(tmp_path)
    raw=review.model_dump();raw['rejected_candidate_ids']=raw['accepted_candidate_ids']
    with pytest.raises(ValidationError):DefectReview(**raw)
    proposal.candidates[0].diameter=4.
    with pytest.raises(ValueError,match='hash'):accepted_candidates(ref,proposal,review,a.image_ref,a.image)
