import numpy as np
import pytest
from photo_worker.algorithms.defect_propose import propose_defects


def fixture():
    image=np.full((512,512,3),.5,np.float32)
    image[200:203,200:203]=.2
    image[300:303,300:303]=[.65,.4,.4]
    return image,np.ones((512,512),np.float32),np.zeros((512,512),np.float32)


def test_dark_red_proposals_and_exact_protection():
    image,skin,protected=fixture()
    result=propose_defects(image,skin,protected,400.)
    assert len(result.candidates)>=2
    assert result.labels[201,201]>0 and result.labels[301,301]>0
    assert not result.labels.flags.writeable and all(0<=c.score<=1 for c in result.candidates)
    protected[190:215,190:215]=1
    again=propose_defects(image,skin,protected,400.)
    assert not np.any(again.labels[190:215,190:215]) and again.labels[301,301]>0


def test_no_textureless_candidates_low_confidence_and_determinism():
    image,skin,protected=fixture()
    empty=propose_defects(np.full_like(image,.5),skin,protected,400.)
    assert not empty.candidates and not np.any(empty.labels)
    low=propose_defects(image,skin,protected,400.,confidence=.5)
    assert low.reason=='low_confidence' and not low.candidates
    a=propose_defects(image,skin,protected,400.);b=propose_defects(image,skin,protected,400.)
    assert a.candidates==b.candidates and np.array_equal(a.labels,b.labels)


def test_invalid_input_and_cancel():
    image,skin,protected=fixture()
    with pytest.raises(ValueError):propose_defects(image,skin,protected,400.,max_work_pixels=1)
    image[0,0,0]=np.nan
    with pytest.raises(ValueError):propose_defects(image,skin,protected,400.)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):propose_defects(image,skin,protected,400.,check_active=cancel)
