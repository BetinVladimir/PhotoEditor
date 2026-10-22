from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage,Rect
from photo_worker.contracts.defects import DefectCandidate,DefectCandidates,candidate_id
from photo_worker.contracts.dust import DustCandidate,DustCandidates,SensorCrop,dust_candidate_id
from photo_worker.contracts.conservative import ConservativeRepairReport
from photo_worker.algorithms.conservative import select_defects,select_dust


def ref(n):return ArtifactRef(artifact_id=uuid4(),sha256=f'{n:064x}')
def image():
    return WorkingImage(pixels=ref(1),canvas=Canvas(width=128,height=128,orientation=1,space_id='source',source_sha256='a'*64),color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='b'*64,encoding='gamma24'),dtype='float32',alpha=None,transform_chain=TransformChain.create([]))
def defect():
    source=image();labels_ref=ref(2);box=Rect(x=62,y=62,width=3,height=3)
    candidate=DefectCandidate(id=candidate_id(source.pixels,labels_ref,1,box,3.),label=1,bbox=box,diameter=3.,score=.95)
    proposal=DefectCandidates(image=ref(3),source_pixels=source.pixels,label_map=labels_ref,canvas=source.canvas,transform_chain=source.transform_chain,candidates=[candidate])
    rgb=np.full((128,128,3),.5,np.float32);rgb[62:65,62:65]=[.65,.49,.49]
    labels=np.zeros((128,128),np.uint16);labels[62:65,62:65]=1
    return proposal,rgb,labels,np.zeros((128,128),np.float32)

def test_strict_red_spot_is_eligible_without_review_but_dark_permanent_mark_is_not():
    proposal,rgb,labels,protected=defect();assert select_defects(proposal,rgb,labels,protected)==tuple(proposal.candidates)
    before=rgb.copy();rgb[62:65,62:65]=[.25,.1,.1]
    assert select_defects(proposal,rgb,labels,protected)==()
    rgb[:]=before;protected[48,48]=.001
    assert select_defects(proposal,rgb,labels,protected)==()

def test_defect_threshold_and_context_are_fixed_not_authored_acceptance():
    proposal,rgb,labels,protected=defect();proposal.candidates[0].score=.89999
    assert select_defects(proposal,rgb,labels,protected)==()
    proposal.candidates[0].score=.95;rgb[46:78:2,46:78]=0
    assert select_defects(proposal,rgb,labels,protected)==()

def dust(count=10,observed=None,radius=1.5):
    frames=[ref(n+10) for n in range(count)];observed=count if observed is None else observed
    camera='raw-'+'c'*64
    candidate=DustCandidate(id=dust_candidate_id(camera,128,128,frames,64.,64.,radius),sensor_x=64.,sensor_y=64.,radius_sensor_px=radius,observed_frames=observed,eligible_frames=count,recurrence=observed/count)
    proposal=DustCandidates(frames=frames,camera_id=camera,sensor_width=128,sensor_height=128,raw_crop=SensorCrop(x=0,y=0,width=128,height=128),candidates=[candidate],warnings=[])
    return proposal,frames[0],image(),[np.zeros((128,128),np.float32)]

def test_dust_requires_ten_frames_full_recurrence_tiny_radius_and_subject_clear_context():
    proposal,source,working,protected=dust();assert select_dust(proposal,source,working,protected)==tuple(proposal.candidates)
    for args in [(9,None,1.5),(10,9,1.5),(10,None,2.01)]:
        p,s,w,m=dust(*args);assert select_dust(p,s,w,m)==()
    protected[0][64,64]=.01;assert select_dust(proposal,source,working,protected)==()
    with pytest.raises(ValueError,match='SOURCE'):select_dust(proposal,ref(99),working,protected)

def test_selection_is_cancellable_and_never_changes_source():
    proposal,rgb,labels,protected=defect();before=rgb.copy()
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):select_defects(proposal,rgb,labels,protected,check_active=cancelled)
    assert np.array_equal(rgb.view(np.uint32),before.view(np.uint32))

def test_automatic_evidence_refuses_duplicate_and_forged_applied_inventory():
    raw=dict(policy='conservative_auto_v1',operation='defects.auto_repair',image=ref(1),proposal=ref(2),resolved=ref(3),selected_candidate_ids=['d'*64],applied_candidate_ids=['d'*64],skipped_candidate_ids=[])
    assert ConservativeRepairReport(**raw).applied_candidate_ids==['d'*64]
    with pytest.raises(ValueError):ConservativeRepairReport(**(raw|{'applied_candidate_ids':['e'*64]}))
    with pytest.raises(ValueError):ConservativeRepairReport(**(raw|{'skipped_candidate_ids':['d'*64]}))
    with pytest.raises(ValueError):ConservativeRepairReport(**(raw|{'selected_candidate_ids':['d'*64,'d'*64]}))
