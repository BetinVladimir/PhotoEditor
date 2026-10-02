from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.layers.alpha import layer_alpha
from photo_worker.runtime.scratch import ScratchSpace


def test_explicit_uint16_conversion_preserves_values_and_cleans_scratch(tmp_path):
    scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0)
    source=np.array([[0,1,32768,65535]],np.uint16);original=source.copy()
    try:
        with layer_alpha(source,source.shape,scratch,mode='normalize_uint16') as result:
            assert result.converted and result.requires_new_reference
            np.testing.assert_array_equal(result.values,source.astype(np.float32)/np.float32(65535))
            assert result.values.dtype==np.float32
        np.testing.assert_array_equal(source,original)
        assert not list(scratch.directory.rglob('*.npy'))
    finally:scratch.cleanup()


def test_legacy_alpha_cannot_be_silently_accepted():
    with pytest.raises(ValueError,match='CONVERSION_REQUIRED'):
        with layer_alpha(np.ones((1,1),np.uint16),(1,1),object(),mode='require_float32'):pass


def test_missing_and_float32_alpha_do_not_allocate():
    with layer_alpha(None,(1,1),object(),mode='require_float32') as result:
        assert result.values is None and not result.requires_new_reference
    source=np.array([[.5]],np.float32)
    with layer_alpha(source,(1,1),object(),mode='require_float32') as result:
        assert result.values is source and not result.converted


def test_invalid_float32_alpha_is_rejected():
    with pytest.raises(ValueError):
        with layer_alpha(np.array([[np.nan]],np.float32),(1,1),object(),mode='require_float32'):pass
