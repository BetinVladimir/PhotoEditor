from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import Face68,MaskRef
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.defects import DefectReview,accepted_candidates
from photo_worker.algorithms.defect_labels import validate_labels
from photo_worker.operations.defects.propose import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_mask_raster_adapter import MaskArtifacts
from test_image_color_adapter import context
from test_propose_prepare import fixture


def setup(root):
    a=MaskArtifacts(root);rgb,image,faces=fixture()
    a.image=image.model_copy(update={'pixels':a.array(root,'proposal-source',rgb)})
    a.image_ref=a.add(a.image);bindings=[]
    for i,entry in enumerate(faces):
        mask=MaskRef(artifact=a.array(root,'proposal-skin-'+str(i),entry.skin_probability),canvas=image.canvas,
            transform_chain=image.transform_chain,selector='skin',confidence=1.,model_lock_id=None,
            object_id=entry.face.object_id,protected=False)
        bindings.append(dict(landmarks=a.add(entry.face).model_dump(mode='json'),skin=mask.model_dump(mode='json')))
    a.raw=dict(image=a.image_ref.model_dump(mode='json'),faces=bindings)
    ctx=context(a);ctx.lease.command.inputs[:]=[r for r in ctx.lease.command.inputs if r is not None]
    ctx.lease.execution_profile['resource_estimate']['ram_bytes']=1073741824
    return a,ctx


def test_proposals_publish_bound_candidates_and_explicit_review(tmp_path):
    a,ctx=setup(tmp_path);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    proposal=a.staged[0][1];report=a.staged[1][1]
    assert len(result.outputs)==5 and result.review_required and proposal.candidates
    assert all(c.decision=='pending' for c in proposal.candidates)
    validate_labels(a.outputs['defect_labels'],proposal)
    assert np.array_equal(a.outputs['defect_labels_png'],a.outputs['defect_labels'])
    assert not np.any(a.outputs['defect_labels'][a.outputs['defect_protection']>0])
    assert report.protection.protected and report.proposal==result.outputs[3]
    assert 'overlapping_candidates' in report.warnings
    review=DefectReview(proposal=report.proposal,image=a.image_ref,label_map=proposal.label_map,revision=1,
        accepted_candidate_ids=[proposal.candidates[0].id],rejected_candidate_ids=[])
    assert accepted_candidates(report.proposal,proposal,review,a.image_ref,a.image)==(proposal.candidates[0],)
    assert ctx.snapshot_progress()==dict(done=3,total=3)


def test_wrong_mask_object_prevents_proposal_uploads(tmp_path):
    a,ctx=setup(tmp_path);a.raw['faces'][0]['skin']['object_id']=str(uuid4())
    with pytest.raises(OperationFailure) as error:execute(ctx,a)
    assert error.value.error.code=='INPUT_INVALID' and not a.uploads


def test_proposals_accept_bound_landmark_collection(tmp_path):
    a,ctx=setup(tmp_path)
    individual=[value for value in a.contracts.values() if isinstance(value,Face68)]
    collection=FaceLandmarks(image=a.image_ref,faces=a.image_ref,landmarks=individual,model_id='fan2d',
        model_lock_id='a'*64,backend='cpu',preprocessing_id='fan2d_crop256_v1',mean_confidence=.9,warnings=[])
    collection_ref=a.add(collection)
    ctx.lease.command.inputs.append(collection_ref)
    for entry,face in zip(a.raw['faces'],individual):
        entry['landmarks']=collection_ref.model_dump(mode='json');entry['object_id']=str(face.object_id)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    assert result.review_required and a.staged[0][1].candidates
