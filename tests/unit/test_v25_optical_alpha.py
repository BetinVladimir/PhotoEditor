from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.optical.alpha import normalized_alpha
from photo_worker.runtime.scratch import ScratchSpace,DiskQuota


def test_missing_and_uint16_alpha_are_reserved_and_exact(tmp_path):
    values=np.array([[0,1,32768,65535]],np.uint16);original=values.copy()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0) as scratch:
        with normalized_alpha(None,(1,4),scratch,tile=1) as opacity:
            assert isinstance(opacity,np.memmap)
            np.testing.assert_array_equal(opacity,1)
        assert not list(scratch.directory.iterdir())
        with normalized_alpha(values,(1,4),scratch,tile=1) as opacity:
            np.testing.assert_array_equal(opacity,values.astype(np.float32)/np.float32(65535))
        np.testing.assert_array_equal(values,original)
        assert not list(scratch.directory.iterdir())


def test_float_alpha_validation_and_cancellation_cleanup(tmp_path):
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0) as scratch:
        values=np.array([[.2,.8]],np.float32)
        with normalized_alpha(values,(1,2),scratch) as opacity:assert opacity is values
        invalid=values.copy();invalid[0,0]=np.nan
        with pytest.raises(ValueError,match='ALPHA_INVALID'):
            with normalized_alpha(invalid,(1,2),scratch):pass
        def cancel():raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):
            with normalized_alpha(None,(1,2),scratch,check_active=cancel):pass
        assert not list(scratch.directory.iterdir())
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1,min_free_bytes=0) as scratch:
        with pytest.raises(DiskQuota):
            with normalized_alpha(None,(1,2),scratch):pass


def test_legacy_source_alpha_flows_through_gamma_lens(tmp_path):
    from photo_worker.operations.optical.prepare import prepare_source_lens
    from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
    from photo_worker.contracts.base import ArtifactRef
    from test_v25_lens_map import profile
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source=WorkingImage(pixels=ref,alpha=ref,dtype='float32',
        canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    rgb=np.array([[[2.,.5,.25],[99.,77.,55.]]],np.float32)
    alpha=np.array([[65535,0]],np.uint16)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0) as scratch:
        with prepare_source_lens(source,profile(),rgb,alpha,1,scratch) as files:
            np.testing.assert_array_equal(np.load(files.alpha,allow_pickle=False),[[1,0]])
            output=np.load(files.rgb,allow_pickle=False)
            np.testing.assert_allclose(output[0,0],rgb[0,0],rtol=1e-6)
            np.testing.assert_array_equal(output[0,1],0)
        assert not list(scratch.directory.iterdir())
        with pytest.raises(ValueError,match='SOURCE_INVALID'):
            with prepare_source_lens(source,profile(),rgb,None,1,scratch):pass
