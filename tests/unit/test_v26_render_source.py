from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_layer_mask_inputs import fixture
from test_layer_prepare import scratch
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.render_source import load_render_source_pixels


@pytest.mark.parametrize('legacy',[False,True])
def test_source_mappings_and_explicit_alpha_conversion_are_scoped(tmp_path,legacy):
    image,*_=fixture(tmp_path)
    image.alpha=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    rgb=tmp_path/'rgb.npy';alpha=tmp_path/'alpha.npy'
    np.save(rgb,np.array([[[-0.,2.,-1.],[1,1,1]]],np.float32))
    np.save(alpha,np.array([[0,65535]],np.uint16) if legacy else np.array([[0,1]],np.float32))
    space=scratch(tmp_path);events=[]
    class Artifacts:
        scratch=space
        @contextmanager
        def input(self,ref,**kw):
            events.append(('open',ref))
            try:yield SimpleNamespace(path=rgb if ref==image.pixels else alpha)
            finally:events.append(('close',ref))
    try:
        with load_render_source_pixels(Artifacts(),image,max_input_bytes=100000,alpha_mode='normalize_uint16' if legacy else 'require_float32') as result:
            assert not result.pixels.rgb.flags.writeable
            np.testing.assert_array_equal(result.pixels.alpha,[[0,1]])
            assert result.alpha_requires_new_reference==legacy
        assert len(events)==4
        assert list(tmp_path.rglob('*.npy'))==[tmp_path/'mask.npy',rgb,alpha]
        events.clear()
        with pytest.raises(ValueError,match='INPUT_BUDGET'):
            with load_render_source_pixels(Artifacts(),image,max_input_bytes=1):pass
        assert events==[]
    finally:space.cleanup()


def test_parent_generated_pixels_are_preserved_and_fractional_coverage_rejected(tmp_path):
    from photo_worker.operations.optical.contracts import ImageResult
    image,mask,mask_ref,_,_,mask_path=fixture(tmp_path)
    mask.selector='generated'
    source_ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    parent=ImageResult(schema_version=1,working_image=source_ref,report=mask_ref,changed_mask=mask_ref,generated_mask=mask_ref)
    rgb=tmp_path/'rgb.npy';np.save(rgb,np.zeros((1,2,3),np.float32))
    np.save(mask_path,np.array([[0,1]],np.float32))
    space=scratch(tmp_path);events=[]
    class Artifacts:
        scratch=space
        def contract(self,ref,model):
            assert ref==mask_ref
            return mask
        @contextmanager
        def input(self,ref,**kw):
            events.append('open')
            try:yield SimpleNamespace(path=rgb if ref==image.pixels else mask_path)
            finally:events.append('close')
    try:
        with load_render_source_pixels(Artifacts(),image,max_input_bytes=100000,source_result=parent,source_ref=source_ref) as result:
            np.testing.assert_array_equal(result.pixels.generated,[[0,1]])
            assert not result.pixels.generated.flags.writeable
        assert events==['open','open','close','close']
        np.save(mask_path,np.array([[0,.5]],np.float32));events.clear()
        with pytest.raises(ValueError,match='NOT_BINARY'):
            with load_render_source_pixels(Artifacts(),image,max_input_bytes=100000,source_result=parent,source_ref=source_ref):pass
        assert events==['open','open','close','close']
        events.clear()
        with pytest.raises(ValueError,match='PARENT_CHANGED'):
            with load_render_source_pixels(Artifacts(),image,max_input_bytes=100000,source_result=parent,source_ref=image.pixels):pass
        assert events==[]
    finally:space.cleanup()
