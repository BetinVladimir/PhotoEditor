from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.v2.portrait_donor_replay import compare_donor_replay


@pytest.mark.parametrize('mode',['ok','wrong_donor','outside','changed'])
def test_exact_donor_reconstruction_comparison_across_tile_boundary(mode):
    source=np.zeros((514,3,3),np.float32);output=source.copy();changed=np.zeros((514,3),np.float32)
    patch=SimpleNamespace(pixels=np.ones((2,2,3),np.float32),changed_mask=np.ones((2,2),np.float32))
    output[511:513,1:3]=patch.pixels;changed[511:513,1:3]=1
    if mode=='wrong_donor':output[512,2,0]=0.5
    elif mode=='outside':output[0,0,0]=-0.0
    elif mode=='changed':changed[512,2]=0
    def run():compare_donor_replay(source,output,changed,patch,(1,511),check_active=lambda:None)
    if mode=='ok':run()
    else:
        with pytest.raises(ValueError,match='DONOR_REPLAY_MISMATCH'):run()
