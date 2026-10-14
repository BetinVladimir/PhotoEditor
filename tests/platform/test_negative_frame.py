from hashlib import sha256
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.optical.negative_frame import convert_negative_frame
from test_optical_negative import calibration
from test_optical_negative_params import intent


def fixture():
    p=intent();raw=canonical_bytes(calibration());p['calibration']['sha256']=sha256(raw).hexdigest()
    return p,raw


def test_full_frame_preserves_alpha_and_source_across_edge_tiles(tmp_path):
    p,raw=fixture();p['strength']=0
    shape=(513,3,3);source=np.random.default_rng(1).random(shape,dtype=np.float32)
    original=source.copy();alpha=np.linspace(0,1,513*3,dtype=np.float32).reshape(513,3)
    output=np.lib.format.open_memmap(tmp_path/'rgb.npy',mode='w+',dtype=np.float32,shape=shape)
    oa=np.lib.format.open_memmap(tmp_path/'alpha.npy',mode='w+',dtype=np.float32,shape=alpha.shape)
    convert_negative_frame(source,output,p,raw,input_icc_sha='c'*64,epsilon=1e-6,
        ram_max_bytes=1<<30,check_active=lambda:None,alpha=alpha,output_alpha=oa)
    np.testing.assert_array_equal(output,original);np.testing.assert_array_equal(oa,alpha)
    np.testing.assert_array_equal(source,original)
    output.flush();oa.flush()


def test_cancel_before_first_tile_write_leaves_output_unpublished():
    p,raw=fixture();scan=np.ones((2,2,3),np.float32);output=np.full_like(scan,-99);calls=[]
    def active():
        calls.append(1)
        if len(calls)==3:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        convert_negative_frame(scan,output,p,raw,input_icc_sha='c'*64,epsilon=1e-6,ram_max_bytes=1<<30,check_active=active)
    np.testing.assert_array_equal(output,-99)


@pytest.mark.parametrize('mode',['alias','alpha_missing','alpha_range'])
def test_frame_rejects_aliases_and_invalid_alpha(mode):
    p,raw=fixture();scan=np.ones((2,2,3),np.float32);output=scan if mode=='alias' else np.zeros_like(scan)
    alpha=np.full((2,2),2,np.float32);oa=None if mode=='alpha_missing' else np.zeros_like(alpha)
    with pytest.raises(ValueError):
        convert_negative_frame(scan,output,p,raw,input_icc_sha='c'*64,epsilon=1e-6,
            ram_max_bytes=1<<30,check_active=lambda:None,alpha=alpha,output_alpha=oa)


@pytest.mark.parametrize('budget,error',[(True,'RESOURCE_INVALID'),(1,'RAM_EXCEEDED')])
def test_negative_frame_ram_refused_before_callbacks_or_writes(budget,error):
    p,raw=fixture();scan=np.ones((2,2,3),np.float32);output=np.full_like(scan,-99)
    def forbidden():pytest.fail('Resource refusal reached activity callback')
    with pytest.raises(ValueError,match=error):
        convert_negative_frame(scan,output,p,raw,input_icc_sha='c'*64,epsilon=1e-6,
            ram_max_bytes=budget,check_active=forbidden)
    np.testing.assert_array_equal(output,-99)
