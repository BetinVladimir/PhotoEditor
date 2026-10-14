import numpy as np
import pytest
from photo_worker.operations.mask.prompt import preprocess,select_candidate


def test_prompt_preprocess_preserves_aspect_and_pads_bottom_right():
    image=np.ones((100,200,3),np.float32);out,scale,w,h=preprocess(image)
    assert (w,h)==(1024,512) and scale==5.12 and np.all(out[:512]>0) and np.all(out[513:]==0)


def test_prompt_candidate_tie_is_lowest_index():
    masks=np.zeros((3,8,8),np.float32);masks[0]=.2;masks[1]=.8
    selected,score,index,count=select_candidate(masks,np.array([.9,.9,.8],np.float32))
    assert index==0 and count==3 and np.allclose(selected,.2) and abs(score-.9)<1e-6


@pytest.mark.parametrize('shape', [(1,0,8), (1,8,0), (1,0,0)])
def test_prompt_candidate_rejects_empty_spatial_output(shape):
    # Refuse before OpenCV resizing, so malformed model output receives the
    # operation's deterministic MODEL_OUTPUT_INVALID failure rather than a
    # generic native-library exception.
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        select_candidate(np.empty(shape,np.float32),np.array([.9],np.float32))


@pytest.mark.parametrize('masks,scores', [
    (np.empty((1,1025,1),np.float32),np.array([.9],np.float32)),
    (np.empty((17,2,2),np.float32),np.ones(17,np.float32)),
    (np.empty((1,2,2),np.float32),np.array([1.1],np.float32)),
    (np.empty((1,2,2),np.float32),np.array([-.1],np.float32)),
    (np.empty((1,2,2),np.float32),np.ones((1,1,1),np.float32)),
    ([[0.,0.],[0.,0.]],np.array([.9],np.float32)),
])
def test_prompt_candidate_refuses_oversized_or_nonprobability_native_output(masks,scores):
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        select_candidate(masks,scores)
