import numpy as np
import pytest
from uuid import uuid4
from photo_worker.operations.optical.negative import invert_negative_tile
from test_optical_negative_params import intent


def calibration():
    return dict(schema_version=1,input_icc_sha='c'*64,density_matrix=np.eye(3).tolist(),
        density_offset=[0,0,0],density_range=[1,1,1],qualification=dict(artifact_id=str(uuid4()),sha256='d'*64))


def test_measured_base_is_black_and_one_density_is_white():
    p=intent();base=np.asarray(p['film_base_rgb'],np.float32)
    scan=np.stack([base,base/np.e]).reshape(1,2,3);original=scan.copy()
    out=invert_negative_tile(scan,p,calibration(),input_icc_sha='c'*64,epsilon=1e-6)
    np.testing.assert_allclose(out[0,0],0,atol=1e-7)
    np.testing.assert_allclose(out[0,1],1,atol=1e-7)
    np.testing.assert_array_equal(scan,original)


def test_strength_zero_preserves_exact_scan_and_exposure_can_be_hdr():
    p=intent();scan=np.zeros((1,1,3),np.float32);p['exposure_ev']=3
    out=invert_negative_tile(scan,p,calibration(),input_icc_sha='c'*64,epsilon=1e-6)
    np.testing.assert_array_equal(out,8)
    p['strength']=0
    np.testing.assert_array_equal(invert_negative_tile(scan,p,calibration(),input_icc_sha='c'*64,epsilon=1e-6),scan)


@pytest.mark.parametrize('mode',['icc','epsilon','negative','nan','oversize','dtype'])
def test_invalid_scan_or_pin_refused(mode):
    scan=np.ones((1,1,3),np.float32);icc='c'*64;eps=1e-6
    if mode=='icc':icc='b'*64
    if mode=='epsilon':eps=True
    if mode=='negative':scan[0,0,0]=-1
    if mode=='nan':scan[0,0,0]=np.nan
    if mode=='oversize':scan=np.ones((513,512,3),np.float32)
    if mode=='dtype':scan=scan.astype(np.float64)
    with pytest.raises(ValueError):invert_negative_tile(scan,intent(),calibration(),input_icc_sha=icc,epsilon=eps)


@pytest.mark.parametrize('strength',[0.25,0.75,1])
def test_asymmetric_color_matrix_offsets_and_exposure_against_scalar_oracle(strength):
    import math
    p=intent();p.update(strength=strength,exposure_ev=1.5)
    c=calibration();c.update(density_matrix=[[1,0.2,-0.1],[-0.3,0.8,0.4],[0.1,-0.2,1.2]],
        density_offset=[0.05,-0.1,0.2],density_range=[0.8,1.1,1.4])
    scan=np.array([[[0.1,0.3,0.05],[0.9,0.7,0.4]],[[0,0.01,0.1],[0.6,0.4,0.15]]],np.float32)
    original=scan.copy();scan.setflags(write=False)
    expected=np.empty_like(scan)
    for y in range(2):
        for x in range(2):
            density=[-math.log(max(float(scan[y,x,i]),1e-6)/p['film_base_rgb'][i]) for i in range(3)]
            for channel in range(3):
                z=sum(c['density_matrix'][channel][i]*density[i] for i in range(3))+c['density_offset'][channel]
                positive=max(0,min(1,z/c['density_range'][channel]))*2**p['exposure_ev']
                expected[y,x,channel]=(1-strength)*float(scan[y,x,channel])+strength*positive
    output=invert_negative_tile(scan,p,c,input_icc_sha='c'*64,epsilon=1e-6)
    np.testing.assert_allclose(output,expected,rtol=1e-6,atol=1e-7)
    np.testing.assert_array_equal(scan,original)
    assert output.dtype==np.float32 and not np.shares_memory(output,scan)


def test_negative_inversion_independent_of_tile_partition():
    scan=np.random.default_rng(9).uniform(0,1,(17,19,3)).astype(np.float32)
    p=intent();c=calibration()
    full=invert_negative_tile(scan,p,c,input_icc_sha='c'*64,epsilon=1e-6)
    tiled=np.empty_like(full)
    for y in range(0,17,7):
        for x in range(0,19,8):
            tiled[y:y+7,x:x+8]=invert_negative_tile(scan[y:y+7,x:x+8],p,c,input_icc_sha='c'*64,epsilon=1e-6)
    np.testing.assert_array_equal(tiled,full)
