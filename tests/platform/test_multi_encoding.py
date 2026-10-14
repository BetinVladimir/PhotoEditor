from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.optical.multi_encoding import prepare_multi_encoding,multi_encoding_resources
from photo_worker.runtime.scratch import ScratchSpace


def test_gamma24_restored_without_hdr_clipping_and_geometry_mask(tmp_path):
    linear=np.full((5,7,3),4.,np.float32);alpha=np.ones((5,7),np.float32)
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='a'*64)
    required=multi_encoding_resources(alpha.shape,alpha.shape,tile=2)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,required['scratch_bytes'],min_free_bytes=0) as scratch:
        with prepare_multi_encoding(linear,alpha,linear,alpha,color,scratch,geometry_changed=True,
                verify_icc=lambda _color:True,ram_max_bytes=required['ram_bytes'],tile=2) as files:
            np.testing.assert_allclose(np.load(files.rgb),4**(1/2.4),rtol=1e-6)
            assert np.load(files.changed_mask).all();path=files.rgb
        assert not path.exists()


def test_unchanged_linear_pixels_have_zero_changed_mask(tmp_path):
    rgb=np.full((5,7,3),-2.,np.float32);alpha=np.ones((5,7),np.float32)
    color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='a'*64)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024**2,min_free_bytes=0) as scratch:
        with prepare_multi_encoding(rgb,alpha,rgb,alpha,color,scratch,geometry_changed=False,
                verify_icc=lambda _color:True,ram_max_bytes=1024**3) as files:
            assert not np.load(files.changed_mask).any()
            np.testing.assert_array_equal(np.load(files.rgb),rgb)


def test_icc_rejection_precedes_scratch():
    class NoScratch:
        def reserve(self,_size):pytest.fail('unexpected allocation')
    rgb=np.ones((5,7,3),np.float32);alpha=np.ones((5,7),np.float32)
    color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='a'*64)
    with pytest.raises(ValueError,match='MULTI_ICC_MISMATCH'):
        with prepare_multi_encoding(rgb,alpha,rgb,alpha,color,NoScratch(),geometry_changed=False,
                verify_icc=lambda _color:False,ram_max_bytes=1024**3):pytest.fail('unexpected result')
