import numpy as np
from photo_worker.algorithms.beauty_fit import fit_face
from photo_worker.operations.pair.fit.defects import measure
from photo_worker.operations.pair.fit.beauty import match_landmarks
from photo_worker.contracts.images import Canvas,Face68,Landmark,TransformChain
from uuid import uuid4


def test_beauty_coordinate_fit_improves_synthetic_target():
    rng=np.random.default_rng(4);source=np.clip(.5+rng.normal(0,.04,(80,80,3)),0,1).astype(np.float32)
    import cv2
    target=cv2.GaussianBlur(source,(0,0),1.4).astype(np.float32);skin=np.ones((80,80),np.float32);protected=np.zeros_like(skin)
    baseline,best=fit_face(source,target,skin,protected,70,1)
    assert best[0]<baseline and best[1][0]>0 and .75<=best[1][2]<=1


def test_defect_evidence_counts_compact_reduction_without_approval():
    source=np.full((64,64,3),.5,np.float32);target=source.copy();source[30:33,30:33]=.15
    components,improved,reduction=measure(source,target,np.ones((64,64),bool))
    assert components>=1 and improved>=1 and reduction>.25


def test_defect_evidence_empty_mask_is_identity_evidence():
    image=np.full((32,32,3),.5,np.float32)
    assert measure(image,image,np.zeros((32,32),bool))==(0,0,0.)


def test_beauty_face_matching_uses_alignment_and_is_one_to_one():
    canvas=Canvas(width=100,height=100,orientation=1,space_id='x',source_sha256='a'*64);chain=TransformChain.create([])
    def face(x):return Face68(object_id=uuid4(),points=[Landmark(x=x,y=.5,confidence=.9) for _ in range(68)],canvas=canvas,transform_chain=chain)
    before=[face(.2),face(.7)];after=[face(.3),face(.8)];matrix=np.array([[1,0,10],[0,1,0],[0,0,1]],float)
    matches=match_landmarks(before,after,matrix,canvas,canvas)
    assert [(a.object_id,b.object_id) for a,b in matches]==[(before[0].object_id,after[0].object_id),(before[1].object_id,after[1].object_id)]
