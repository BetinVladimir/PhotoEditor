from types import SimpleNamespace
import numpy as np
import pytest
from test_layer_prepare import scratch
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_files import prepare_render_output_files


@pytest.mark.parametrize('changed,converted',[(True,False),(False,False),(False,True)])
def test_final_files_reuse_unchanged_refs_and_preserve_generated_coverage(tmp_path,changed,converted):
    pixels=AdjustmentPixels(np.array([[[-0.,2.,-1.]]],np.float32),np.ones((1,1),np.float32),np.ones((1,1),np.float32))
    changes=SimpleNamespace(rgb_changed=changed,alpha_changed=False)
    prepared=SimpleNamespace(composition=SimpleNamespace(pixels=pixels,changes=changes),alpha_requires_new_reference=converted)
    space=scratch(tmp_path)
    try:
        with prepare_render_output_files(prepared,space) as files:
            assert (files.rgb is not None)==changed and (files.alpha is not None)==converted
            if changed:np.testing.assert_array_equal(np.load(files.rgb).view(np.uint32),pixels.rgb.view(np.uint32))
            np.testing.assert_array_equal(np.load(files.generated),1)
            assert files.changes is changes
        assert not list(tmp_path.rglob('*.npy'))
        with pytest.raises(RuntimeError):
            with prepare_render_output_files(prepared,space):raise RuntimeError('stage failed')
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()
