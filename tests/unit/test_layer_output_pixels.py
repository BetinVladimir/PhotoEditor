import numpy as np
import pytest
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.layers.output_validation import validate_composed_pixels


def case():
    source=np.zeros((513,2,3),np.float32);source[0,0]=-0.
    proposal=np.full_like(source,4.)
    mask=np.ones((513,2),np.float32);protected=np.zeros_like(mask);protected[0,0]=.2
    output=proposal.copy();output[0,0]=source[0,0]
    changed=np.ones_like(mask);changed[0,0]=0
    options=dict(color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='a'*64),
                 opacity=1,blend='normal',protected=protected)
    return output,changed,source,proposal,mask,options


def test_hdr_protected_signed_zero_and_last_tile_exact():
    *arrays,options=case()
    validate_composed_pixels(*arrays,**options)


@pytest.mark.parametrize('target',['rgb','changed','signed_zero','alpha'])
def test_mutated_outputs_are_rejected(target):
    output,changed,source,proposal,mask,options=case()
    if target=='rgb':output[512,0,0]=3.
    elif target=='changed':changed[512,0]=0
    elif target=='signed_zero':output[0,0]=0.
    else:options['output_alpha']=np.ones_like(mask)
    with pytest.raises(ValueError):validate_composed_pixels(output,changed,source,proposal,mask,**options)


def test_alpha_only_change_requires_mask_and_output_alpha():
    output,changed,source,proposal,mask,options=case();proposal[:]=source
    changed[:]=1;changed[0,0]=0
    alpha=np.full_like(mask,.5);alpha[0,0]=1
    validate_composed_pixels(source,changed,source,proposal,mask,output_alpha=alpha,
                            proposal_alpha=np.full_like(mask,.5),**options)
    with pytest.raises(ValueError,match='LAYER_OUTPUT_ALPHA_INVALID'):
        validate_composed_pixels(source,changed,source,proposal,mask,
                                proposal_alpha=np.full_like(mask,.5),**options)
