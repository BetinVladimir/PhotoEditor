from types import SimpleNamespace
from uuid import uuid4
import numpy as np
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_protection import preserve_render_protection


def test_positive_protection_restores_rgb_and_opaque_alpha_exactly(tmp_path):
    before=AdjustmentPixels(np.full((1,2,3),-.0,np.float32),None)
    after=AdjustmentPixels(np.ones((1,2,3),np.float32),np.full((1,2),.4,np.float32))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,100000,min_free_bytes=0) as scratch:
        with preserve_render_protection(SimpleNamespace(scratch=scratch),before,after,
                lambda *args:np.array([[.001,0]],np.float32)) as result:
            np.testing.assert_array_equal(result.rgb[:,0].view(np.uint32),before.rgb[:,0].view(np.uint32))
            np.testing.assert_array_equal(result.rgb[:,1],after.rgb[:,1])
            np.testing.assert_array_equal(result.alpha,np.array([[1,.4]],np.float32))
        assert not list(scratch.directory.iterdir())
