from contextlib import contextmanager
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.optical.cylindrical_stack import prepare_cylindrical_panorama, cylindrical_projection_resources
from photo_worker.runtime.scratch import ScratchSpace


def test_cylindrical_stack_preserves_each_original_inverse_field(tmp_path,monkeypatch):
    import photo_worker.operations.optical.cylindrical_stack as module
    sentinel=object()
    @contextmanager
    def automatic(frames,alphas,reference,scratch,**kwargs):
        assert len(frames)==2 and reference==1 and kwargs['operation']=='panorama'
        assert not frames[0].flags.writeable and not alphas[0][:,0].any()
        yield sentinel
    monkeypatch.setattr(module,'prepare_automatic_planar_stack',automatic)
    rgb=np.full((51,101,3),4.,np.float32);alpha=np.ones((51,101),np.float32)
    required=cylindrical_projection_resources([alpha.shape]*2,tile=7)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,required['scratch_bytes']+1024**2,min_free_bytes=0) as scratch:
        with prepare_cylindrical_panorama([rgb]*2,[alpha]*2,1,scratch,max_pixels=100000,
                ram_max_bytes=2**40,registration_error_px=1.,match_inlier_min=.8,
                motion_threshold=.5,luma=(.2126,.7152,.0722),tile=7) as result:
            assert result.panorama is sentinel and len(result.projections)==2
            paths=[p.inverse_field for p in result.projections]
            assert paths[0]!=paths[1]
            for path in paths:np.testing.assert_array_equal(np.load(path)[25,50],[50.5,25.5])
        assert all(not p.exists() for p in paths)


def test_blank_projected_stack_rejection_cleans_all_files(tmp_path):
    rgb=np.zeros((100,120,3),np.float32);alpha=np.ones((100,120),np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,16*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='UNREGISTERED_FRAME'):
            with prepare_cylindrical_panorama([rgb]*2,[alpha]*2,0,scratch,max_pixels=100000,
                    ram_max_bytes=2**40,registration_error_px=1.,match_inlier_min=.8,
                    motion_threshold=.5,luma=(.2126,.7152,.0722)):
                pytest.fail('unexpected result')
        assert not list(scratch.directory.iterdir())


def test_invalid_motion_threshold_refuses_before_any_projection():
    rgb=np.ones((51,101,3),np.float32);alpha=np.ones((51,101),np.float32)
    class NoScratch:
        def reserve(self,_size):pytest.fail('projection allocated scratch')
    with pytest.raises(ValueError,match='MULTI_MOTION_INVALID'):
        with prepare_cylindrical_panorama([rgb]*2,[alpha]*2,0,NoScratch(),max_pixels=100000,
                ram_max_bytes=2**40,registration_error_px=1.,match_inlier_min=.8,
                motion_threshold=float('nan'),luma=(.2126,.7152,.0722)):
            pytest.fail('unexpected projection')
