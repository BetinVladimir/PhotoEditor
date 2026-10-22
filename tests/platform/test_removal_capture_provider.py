"""Only the controlled infer boundary; alpha remains the production pipeline's job."""
import importlib.util
from pathlib import Path
import numpy as np
import pytest

spec=importlib.util.spec_from_file_location('removal_capture_provider',
    Path(__file__).resolve().parents[1]/'integration/removal_capture_provider.py')
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


@pytest.mark.parametrize('lock',[None,1,'','a'*63,'a'*65,'G'*64,'A'*64,' '*64])
def test_invalid_frozen_lock_rejected(lock):
    with pytest.raises(ValueError,match='exact frozen lock'):
        module.controlled_pool(lock)


@pytest.mark.parametrize('model,backend',[('other','cpu'),('big_lama','cuda'),('big_lama','mps'),('big_lama','CPU'),(None,'cpu')])
def test_only_explicit_model_and_backend(model,backend):
    pool=module.controlled_pool('a'*64)
    assert pool.lock.lock_id=='a'*64
    with pytest.raises(ValueError,match='explicit big_lama CPU'):
        with pool.acquire(model,backend):
            pytest.fail('wrong model/backend must not yield an engine')


@pytest.mark.parametrize('kind',['float64','rgba','flat','mask_shape','mask_dtype','image_list','mask_list'])
def test_invalid_dtype_or_shape_rejected(kind):
    image=np.full((4,5,3),.5,np.float32)
    mask=np.ones((4,5),np.float32)
    if kind=='float64':image=image.astype(np.float64)
    elif kind=='rgba':image=np.full((4,5,4),.5,np.float32)
    elif kind=='flat':image=np.zeros((20,),np.float32)
    elif kind=='mask_shape':mask=np.ones((5,4),np.float32)
    elif kind=='mask_dtype':mask=mask.astype(np.bool_)
    elif kind=='image_list':image=image.tolist()
    elif kind=='mask_list':mask=mask.tolist()
    with module.controlled_pool('a'*64).acquire('big_lama','cpu') as engine:
        with pytest.raises(ValueError,match='controlled model input invalid'):
            engine.infer(image,mask)


def test_output_changes_only_mask_and_owns_its_storage():
    image=np.full((4,5,3),.5,np.float32);image[0,0]=.99
    mask=np.zeros((4,5),np.float32);mask[0,0]=1;mask[2,3]=1
    before=image.copy();original_mask=mask.copy()
    with module.controlled_pool('b'*64).acquire('big_lama','cpu') as engine:
        output=engine.infer(image,mask)
    assert output.shape==image.shape and output.dtype==np.float32
    np.testing.assert_array_equal(output[mask==0],before[mask==0])
    assert np.all(output[mask>0]>before[mask>0])
    assert np.all(output<=1)
    np.testing.assert_array_equal(image,before)
    np.testing.assert_array_equal(mask,original_mask)
    assert not np.shares_memory(image,output)
    output[:]=0
    np.testing.assert_array_equal(image,before)
    # No alpha argument exists: RGBA refusal above keeps alpha ownership in the
    # actual WorkingImage/production preservation+staging path, not this double.
