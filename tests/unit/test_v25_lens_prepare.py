from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.optical.prepare import prepare_lens
from photo_worker.runtime.scratch import ScratchSpace, DiskQuota
from photo_worker.runtime.arrays import read_array, read_inverse_field
from test_v25_lens_map import profile


def test_lens_files_are_exact_readable_and_owned(tmp_path):
    scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0)
    rgb=np.arange(18,dtype=np.float32).reshape(2,3,3)
    alpha=np.ones((2,3),dtype=np.float32)
    try:
        with prepare_lens(profile(),rgb,alpha,0,scratch,tile=1) as files:
            np.testing.assert_array_equal(np.load(files.rgb,allow_pickle=False),rgb)
            np.testing.assert_array_equal(np.load(files.alpha,allow_pickle=False),alpha)
            x,y=np.meshgrid(np.arange(3)+.5,np.arange(2)+.5)
            np.testing.assert_array_equal(np.load(files.inverse_field,allow_pickle=False),np.stack((x,y),axis=-1))
            with read_array(files.rgb,(2,3,3),'float32',lambda:None) as mapped:
                np.testing.assert_array_equal(mapped,rgb)
            with read_array(files.alpha,(2,3),'float32',lambda:None,unit_interval=True) as mapped:
                np.testing.assert_array_equal(mapped,alpha)
            with read_inverse_field(files.inverse_field,(2,3),lambda:None) as mapped:
                assert mapped.shape==(2,3,2)
            files.reservation.validate()
            assert not files.uncovered
            np.testing.assert_array_equal(np.load(files.changed_mask,allow_pickle=False),0)
        assert not files.rgb.exists() and not files.alpha.exists() and not files.inverse_field.exists() and not files.changed_mask.exists()
    finally:scratch.cleanup()


def test_cancellation_and_quota_do_not_leak_files(tmp_path):
    rgb=np.ones((2,3,3),dtype=np.float32);alpha=np.ones((2,3),dtype=np.float32)
    scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0)
    calls=0
    def cancel():
        nonlocal calls
        calls+=1
        if calls==5:raise RuntimeError('cancelled')
    try:
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_lens(profile(),rgb,alpha,1,scratch,tile=1,check_active=cancel):pass
        assert not list(scratch.directory.iterdir())
    finally:scratch.cleanup()
    scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,100,min_free_bytes=0)
    try:
        with pytest.raises(DiskQuota):
            with prepare_lens(profile(),rgb,alpha,1,scratch):pass
        assert not list(scratch.directory.iterdir())
    finally:scratch.cleanup()


def test_changed_mask_reports_rgb_and_alpha_bits(tmp_path):
    rgb=np.full((3,3,3),2,dtype=np.float32);alpha=np.ones((3,3),dtype=np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0) as scratch:
        with prepare_lens(profile(k=[.2,0,0]),rgb,alpha,1,scratch) as files:
            output=np.load(files.rgb,allow_pickle=False)
            opacity=np.load(files.alpha,allow_pickle=False)
            expected=(np.any(output.view(np.uint32)!=rgb.view(np.uint32),axis=-1)|
                      (opacity.view(np.uint32)!=alpha.view(np.uint32))).astype(np.float32)
            np.testing.assert_array_equal(np.load(files.changed_mask,allow_pickle=False),expected)
            assert expected[0,0]==1 and expected[1,1]==0


def test_working_gamma_preparation_preserves_bypass_and_linear_interpolation(tmp_path):
    from photo_worker.operations.optical.prepare import prepare_working_lens
    from photo_worker.operations.optical.color import to_working_linear,from_working_linear
    from photo_worker.contracts.images import ColorSpace
    from photo_worker.algorithms.lens import resample_lens_tiles
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='a'*64)
    rgb=np.linspace(-.2,2,48,dtype=np.float32).reshape(4,4,3)
    alpha=np.ones((4,4),dtype=np.float32);p=profile(k=[.2,0,0])
    expected=next(resample_lens_tiles(p,to_working_linear(rgb,color),alpha,1))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0) as scratch:
        with prepare_working_lens(p,rgb,alpha,color,1,scratch,tile=2) as files:
            output=np.load(files.rgb,allow_pickle=False)
            np.testing.assert_array_equal(output,from_working_linear(expected.rgb,color))
            mask=np.load(files.changed_mask,allow_pickle=False)
            opacity=np.load(files.alpha,allow_pickle=False)
            bits=(np.any(rgb.view(np.uint32)!=output.view(np.uint32),axis=-1)|
                  (alpha.view(np.uint32)!=opacity.view(np.uint32))).astype(np.float32)
            np.testing.assert_array_equal(mask,bits)
        assert not list(scratch.directory.iterdir())
        with prepare_working_lens(p,rgb,alpha,color,0,scratch,tile=2) as files:
            np.testing.assert_array_equal(np.load(files.rgb,allow_pickle=False).view(np.uint32),rgb.view(np.uint32))
            np.testing.assert_array_equal(np.load(files.changed_mask,allow_pickle=False),0)
