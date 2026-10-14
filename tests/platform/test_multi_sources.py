from pathlib import Path
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.optical.multi_sources import prepare_multi_sources,multi_source_resources
from photo_worker.runtime.scratch import ScratchSpace


def source():
    return WorkingImage.model_validate_json((Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-source.json').read_bytes())


def test_source_batch_normalizes_legacy_alpha_and_preserves_hdr(tmp_path):
    image=source();rgb=np.full((100,100,3),-2.,np.float32);alpha=np.full((100,100),32768,np.uint16)
    required=multi_source_resources([(100,100)]*2,tile=17)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,required['scratch_bytes'],min_free_bytes=0) as scratch:
        with prepare_multi_sources([image]*2,[rgb]*2,[alpha]*2,scratch,
                verify_icc=lambda _color:True,ram_max_bytes=required['ram_bytes'],tile=17) as result:
            assert result.luma==(.2126,.7152,.0722)
            np.testing.assert_array_equal(result.rgb[0],rgb)
            np.testing.assert_allclose(result.alpha[0],np.float32(32768)/np.float32(65535),rtol=0,atol=0)
        assert not list(scratch.directory.iterdir())


def test_icc_failure_precedes_source_files():
    class NoScratch:
        budget_bytes=2**40
        def reserve(self,_size):pytest.fail('unexpected allocation')
    image=source();rgb=np.zeros((100,100,3),np.float32);alpha=np.ones((100,100),np.float32)
    with pytest.raises(ValueError,match='MULTI_ICC_MISMATCH'):
        with prepare_multi_sources([image]*2,[rgb]*2,[alpha]*2,NoScratch(),
                verify_icc=lambda _color:False,ram_max_bytes=2**40):pytest.fail('unexpected result')


def test_source_metadata_cannot_hide_missing_alpha():
    image=source();rgb=np.zeros((100,100,3),np.float32)
    with pytest.raises(ValueError,match='MULTI_SOURCE_INVALID'):
        with prepare_multi_sources([image]*2,[rgb]*2,[None,None],None,
                verify_icc=lambda _color:True,ram_max_bytes=2**40):pytest.fail('unexpected result')
