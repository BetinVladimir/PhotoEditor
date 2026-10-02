from pathlib import Path
import struct
import numpy as np
import pytest
from photo_worker.runtime.arrays import read_array


def test_exact_array_layout_and_mapping_lifetime(tmp_path):
    path=tmp_path/'rgb.npy';values=np.arange(18,dtype=np.float32).reshape(2,3,3)
    np.save(path,values)
    with read_array(path,(2,3,3),'float32',lambda:None) as mapped:
        assert isinstance(mapped,np.memmap) and not mapped.flags.writeable
        assert np.array_equal(mapped,values)
        underlying=mapped._mmap
    assert underlying.closed


@pytest.mark.parametrize('bad',['object','dtype','shape','fortran','nan','tail','truncated','huge_header'])
def test_reject_malformed_array_without_inference(tmp_path,bad):
    path=tmp_path/'bad.npy';array=np.zeros((2,3,3),np.float32)
    if bad=='object':array=array.astype(object)
    if bad=='dtype':array=array.astype(np.float64)
    if bad=='shape':array=np.zeros((3,3,3),np.float32)
    if bad=='fortran':array=np.asfortranarray(array)
    if bad=='nan':array[0,0,0]=np.nan
    np.save(path,array)
    if bad=='tail':path.write_bytes(path.read_bytes()+b'x')
    if bad=='truncated':path.write_bytes(path.read_bytes()[:-1])
    if bad=='huge_header':path.write_bytes(b'\x93NUMPY\x02\x00'+struct.pack('<I',2**32-1))
    with pytest.raises(ValueError):
        with read_array(path,(2,3,3),'float32',lambda:None):pytest.fail('Malformed array was accepted')


def test_mask_range_and_cancelled_mapping(tmp_path):
    path=tmp_path/'mask.npy';np.save(path,np.full((3,4),1.01,np.float32))
    with pytest.raises(ValueError,match='RANGE'):
        with read_array(path,(3,4),'float32',lambda:None,unit_interval=True):pass
    calls=0
    def check():
        nonlocal calls
        calls+=1
        if calls==2:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        with read_array(path,(3,4),'float32',check):pass
    with read_array(path,(3,4),'float32',lambda:None) as mapped:assert mapped.shape==(3,4)


@pytest.mark.parametrize('size',[33,65])
def test_absolute_lut_reader_checks_shape_range_and_lifetime(tmp_path,size):
    from photo_worker.runtime.arrays import read_lut
    path=tmp_path/'lut.npy'
    values=np.random.default_rng(2).random((size,size,size,3),dtype=np.float32)
    np.save(path,values)
    with read_lut(path,size,lambda:None) as lut:
        assert np.array_equal(lut,values) and not lut.flags.writeable
        mapping=lut._mmap
    assert mapping.closed
    values[0,0,0,0]=1.01;np.save(path,values)
    with pytest.raises(ValueError,match='RANGE'):
        with read_lut(path,size,lambda:None):pass


def test_lut_reader_rejects_image_layout_and_residual_size(tmp_path):
    from photo_worker.runtime.arrays import read_lut
    path=tmp_path/'image.npy';np.save(path,np.zeros((33,33,3),np.float32))
    with pytest.raises(ValueError,match='LAYOUT'):
        with read_lut(path,33,lambda:None):pass
    with pytest.raises(ValueError,match='LUT_SIZE'):
        with read_lut(path,17,lambda:None):pass
    with pytest.raises(ValueError,match='SHAPE'):
        with read_array(path,(33,33,33,3),'float32',lambda:None):pass
