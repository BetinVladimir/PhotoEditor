from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.region.portrait import prepare_region_faces
from photo_worker.runtime.scratch import ScratchSpace
from test_region_preview_presentation import fixture


@pytest.mark.parametrize('backend',['cuda','mps'])
def test_native_region_noface_detection_preserves_exact_model_backend(tmp_path,backend):
    params,raw=fixture(False);source=params.validate_source(raw)
    calls=[]
    class Pool:
        lock=SimpleNamespace(lock_id=params.frozen_recipe.model_lock_sha)
        @contextmanager
        def acquire(self,model,selected):
            calls.append((model,selected))
            assert selected==backend
            yield SimpleNamespace(infer=lambda value:np.empty((0,5),np.float32))
    rgb=np.full((source.canvas.height,source.canvas.width,3),.4,np.float32)
    original=rgb.copy()
    root=tmp_path/'scratch';root.mkdir()
    checks=[]
    with ScratchSpace(root,uuid4(),uuid4(),1,20_000_000,min_free_bytes=0) as scratch:
        with prepare_region_faces(rgb,None,source,Pool(),backend,Pool.lock.lock_id,scratch,
                verify_runtime=lambda:checks.append(True)) as faces:
            assert faces==()
        assert not list(scratch.directory.iterdir())
    assert calls==[('sfd',backend),('fan2d',backend)]
    assert len(checks)>=4 and np.array_equal(rgb.view(np.uint32),original.view(np.uint32))


def test_native_region_face_analysis_refuses_cross_lock_before_model_io(tmp_path):
    params,raw=fixture(False);source=params.validate_source(raw)
    class Pool:
        lock=SimpleNamespace(lock_id='0'*64)
        def acquire(self,*args):raise AssertionError('model IO before binding')
    with pytest.raises(ValueError,match='REGION_FACE_MODEL_LOCK_INVALID'):
        with prepare_region_faces(np.zeros((300,400,3),np.float32),None,source,Pool(),'mps',
                params.frozen_recipe.model_lock_sha,None,verify_runtime=lambda:None):pass
