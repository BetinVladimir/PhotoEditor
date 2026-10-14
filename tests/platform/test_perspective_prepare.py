from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import ColorSpace
from photo_worker.algorithms.perspective import manual_perspective
from photo_worker.operations.optical.perspective_prepare import prepare_source_perspective
from photo_worker.runtime.scratch import ScratchSpace,DiskQuota


def source(width=4,height=3,alpha=None,encoding='linear'):
    return SimpleNamespace(dtype='float32',alpha=alpha,canvas=SimpleNamespace(width=width,height=height),
        color=ColorSpace(profile_id='linear_srgb_v1' if encoding=='linear' else 'work_rec2020_gamma24_v1',encoding=encoding,icc_sha256='a'*64))


def scratch(tmp_path,budget=1000000):
    return ScratchSpace(tmp_path,uuid4(),uuid4(),1,budget,min_free_bytes=0)


def test_identity_gamma_preserves_bits_and_releases_files(tmp_path):
    image=source(encoding='gamma24');rgb=np.arange(36,dtype=np.float32).reshape(3,4,3)/17
    geometry=manual_perspective([[0,0],[1,0],[1,1],[0,1]],4,3,max_crop_fraction=0,max_pixels=12)
    with scratch(tmp_path) as space:
        with prepare_source_perspective(image,rgb,None,geometry,space,tile=2) as files:
            np.testing.assert_array_equal(np.load(files.rgb).view(np.uint32),rgb.view(np.uint32))
            assert not np.load(files.changed_mask).any()
            paths=(files.rgb,files.alpha,files.changed_mask)
        assert all(not path.exists() for path in paths)


def test_geometry_changes_are_marked_and_alpha_normalized(tmp_path):
    image=source(alpha=object());rgb=np.ones((3,4,3),np.float32)*2
    alpha=np.full((3,4),32768,np.uint16)
    geometry=manual_perspective([[.25,0],[.75,0],[.75,1],[.25,1]],4,3,max_crop_fraction=.5,max_pixels=12)
    with scratch(tmp_path) as space:
        with prepare_source_perspective(image,rgb,alpha,geometry,space) as files:
            assert np.load(files.changed_mask).all()
            np.testing.assert_allclose(np.load(files.alpha),32768/65535)
            np.testing.assert_allclose(np.load(files.rgb),2)
            assert files.clipping_fraction==1


def test_scratch_budget_rejects_before_result(tmp_path):
    geometry=manual_perspective([[0,0],[1,0],[1,1],[0,1]],4,3,max_crop_fraction=0,max_pixels=12)
    with scratch(tmp_path,100) as space:
        with pytest.raises(DiskQuota):
            with prepare_source_perspective(source(),np.ones((3,4,3),np.float32),None,geometry,space):pass


def test_consumer_failure_releases_result_files(tmp_path):
    geometry=manual_perspective([[0,0],[1,0],[1,1],[0,1]],4,3,max_crop_fraction=0,max_pixels=12)
    with scratch(tmp_path) as space:
        with pytest.raises(RuntimeError,match='stage failed'):
            with prepare_source_perspective(source(),np.ones((3,4,3),np.float32),None,geometry,space) as files:
                paths=(files.rgb,files.alpha,files.changed_mask)
                raise RuntimeError('stage failed')
        assert all(not path.exists() for path in paths)
