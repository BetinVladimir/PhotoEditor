from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.operations.learning.skin import open_holdout_skin
from photo_worker.operations.session.contracts import FaceObservation, FaceObservations, FacePoint, FaceRect
from photo_worker.runtime.scratch import ScratchSpace
from test_v23_learning_source import _fixture


@pytest.mark.parametrize('mode', ['valid', 'missing', 'lock', 'cancel', 'low-confidence'])
def test_holdout_skin_requires_pinned_model_and_releases_scopes(tmp_path, mode):
    artifacts, case = _fixture(tmp_path)
    observation=FaceObservation(observation_id=uuid4(),face_id=uuid4(),
        box=FaceRect(x0=.1,y0=.1,x1=.9,y1=.9),
        landmarks=[FacePoint(x=.5,y=.5)]*68,embedding=None,quality=1,person_id=None)
    observations=FaceObservations(schema_version=1,binding=case.binding,faces=[observation])
    events=[];cancelled=False
    class Engine:
        def infer(self,crop):
            nonlocal cancelled
            events.append('infer')
            assert crop.shape==(512,512,3) and crop.dtype==np.float32
            if mode=='cancel':cancelled=True
            logits=np.zeros((19,512,512),np.float32);logits[1]=0 if mode=='low-confidence' else 10
            return logits
    @contextmanager
    def acquire(model,backend):
        events.append('open')
        assert model=='bisenet_face19' and backend=='cuda'
        try:yield Engine()
        finally:events.append('close')
    pool=SimpleNamespace(lock=SimpleNamespace(lock_id='c'*64,
        models=[] if mode=='missing' else [SimpleNamespace(id='bisenet_face19')]),acquire=acquire)
    def active():
        if cancelled:raise RuntimeError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:
        def run():
            with open_holdout_skin(artifacts.rgb,observations,pool,'cuda',
                    'd'*64 if mode=='lock' else 'c'*64,scratch,active) as skin:
                assert skin.shape==artifacts.rgb.shape[:2]
                assert skin.dtype==np.float32 and not skin.flags.writeable
                assert np.any(skin>0) and np.all((skin==0)|(skin==1))
        if mode=='valid':run()
        elif mode=='cancel':
            with pytest.raises(RuntimeError,match='cancelled'):run()
        else:
            with pytest.raises(ValueError,match='HOLDOUT_SKIN_'):run()
        assert list(scratch.directory.iterdir())==[]
    assert events==(['open','infer','close'] if mode in ('valid','cancel','low-confidence') else [])
