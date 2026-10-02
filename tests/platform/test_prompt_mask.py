import numpy as np
from photo_worker.operations.mask.prompt import preprocess,select_candidate


def test_prompt_preprocess_preserves_aspect_and_pads_bottom_right():
    image=np.ones((100,200,3),np.float32);out,scale,w,h=preprocess(image)
    assert (w,h)==(1024,512) and scale==5.12 and np.all(out[:512]>0) and np.all(out[513:]==0)


def test_prompt_candidate_tie_is_lowest_index():
    masks=np.zeros((3,8,8),np.float32);masks[0]=.2;masks[1]=.8
    selected,score,index,count=select_candidate(masks,np.array([.9,.9,.8],np.float32))
    assert index==0 and count==3 and np.allclose(selected,.2) and abs(score-.9)<1e-6
