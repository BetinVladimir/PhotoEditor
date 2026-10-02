from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.portrait.prepare import prepare_beauty
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_beauty import prepare_beauty_stage
from test_beauty_prepare import inputs
from test_beauty import params


def test_existing_multi_face_pixels_and_evidence_preserved_exactly(tmp_path):
    rgb,image,faces,_=inputs()
    alpha=np.full(rgb.shape[:2],.7,np.float32);generated=np.zeros(rgb.shape[:2],np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,20*1024**2,min_free_bytes=0) as scratch:
        with prepare_beauty(rgb,image,faces,params(),scratch) as legacy:
            expected=np.load(legacy.image,allow_pickle=False).copy();evidence=legacy.faces
        with prepare_beauty_stage(SimpleNamespace(scratch=scratch),image,AdjustmentPixels(rgb,alpha,generated),
                params(),tuple(faces)) as result:
            np.testing.assert_array_equal(result.pixels.rgb,expected)
            assert result.faces==evidence and all(face.applied for face in result.faces)
            assert result.pixels.alpha is alpha and result.pixels.generated is generated
            assert not result.review_required
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('failure',['consumer','cancel'])
def test_face_stage_failure_releases_roi_output_and_maps(tmp_path,failure):
    rgb,image,faces,_=inputs()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,20*1024**2,min_free_bytes=0) as scratch:
        def active():
            if failure=='cancel' and any(p.name.startswith('face-delta-') for p in scratch.directory.iterdir()):
                raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError):
            with prepare_beauty_stage(SimpleNamespace(scratch=scratch),image,AdjustmentPixels(rgb,None),
                    params(),tuple(faces),check_active=active):
                raise RuntimeError('consumer failed')
        assert not list(scratch.directory.iterdir())
