from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.defects import DefectCandidates,DefectReview,candidate_id
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.style import Defects,Disabled
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.operations.defects.repair import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from test_defect_review import setup as proposal_setup
from test_defect_labels import label_map
from test_image_color_adapter import context


def setup(root,enabled=True):
    a,_,old,_=proposal_setup(root);labels=label_map(old)
    a.rgb=np.full((*labels.shape,3),.5,np.float32);a.rgb[labels>0]=.1
    a.image=a.image.model_copy(update={'pixels':a.array(root,'defect-rgb',a.rgb)});a.image_ref=a.add(a.image)
    label_ref=a.array(root,'defect-labels',labels)
    candidates=[c.model_copy(update={'id':candidate_id(a.image.pixels,label_ref,c.label,c.bbox,c.diameter)}) for c in old.candidates]
    proposal=DefectCandidates(image=a.image_ref,source_pixels=a.image.pixels,label_map=label_ref,canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,candidates=candidates)
    proposal_ref=a.add(proposal)
    review=DefectReview(proposal=proposal_ref,image=a.image_ref,label_map=label_ref,revision=2,
        accepted_candidate_ids=[candidates[1].id],rejected_candidate_ids=[candidates[0].id])
    a.review=review;a.labels=labels
    protected=MaskRef(artifact=a.array(root,'defect-protected',np.zeros(labels.shape,np.float32)),canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=True)
    body=a.resolved.model_dump(exclude={'hash'});body['params']['image']=a.image_ref;body['strength']=.5
    body['modules']['defects']=Defects(enabled=True,algorithm='defectrepair_telea_v1',policy='review',strength=.8,
        diameter_face_fraction=[.002,.012],preserve_permanent_marks=True) if enabled else Disabled()
    a.resolved=ResolvedStyle.create(**body)
    a.raw=dict(image=a.image_ref.model_dump(mode='json'),proposal=proposal_ref.model_dump(mode='json'),review=a.add(review).model_dump(mode='json'),
        resolved=a.add(a.resolved).model_dump(mode='json'),protected=protected.model_dump(mode='json'))
    ctx=context(a);ctx.lease.execution_profile['resource_estimate']['ram_bytes']=1073741824
    return a,ctx


def test_task_repairs_only_approved_with_strength_once(tmp_path):
    a,ctx=setup(tmp_path);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    assert np.array_equal(a.output[a.labels!=2],a.rgb[a.labels!=2])
    assert np.mean(a.output[a.labels==2])>.2
    report=a.staged[1][1]
    assert report.review_revision==2 and report.requested_strength==.8 and report.effective_strength==.4
    assert len(report.candidates)==1 and report.candidates[0].label==2
    assert len(result.outputs)==3 and not result.review_required
    assert a.staged[0][1].alpha==a.image.alpha


def test_stale_review_rejected_before_io(tmp_path):
    a,ctx=setup(tmp_path)
    wrong=a.review.model_copy(update={'label_map':ArtifactRef(artifact_id=uuid4(),sha256='e'*64)})
    a.raw['review']=a.add(wrong).model_dump(mode='json')
    with pytest.raises(OperationFailure) as error:execute(ctx,a)
    assert error.value.error.code=='STALE_DEFECT_REVIEW' and not a.uploads


def test_disabled_repair_keeps_source_without_download(tmp_path):
    a,ctx=setup(tmp_path,False);root=tmp_path/'scratch';root.mkdir()
    def forbidden(*args,**kwargs):raise AssertionError('Unexpected pixel IO')
    a.input=forbidden
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    assert a.staged[0][1]==a.image and not a.uploads and len(result.outputs)==2
    assert a.staged[1][1].warnings==['module_disabled']
