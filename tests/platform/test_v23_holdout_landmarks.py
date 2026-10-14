from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.operations.face.landmarks import _crop, _points
from photo_worker.operations.learning.beauty_render import recover_holdout_landmarks
from photo_worker.operations.session.describe import _preview
from photo_worker.operations.session.contracts import FaceObservation, FaceObservations, FacePoint, FaceRect
from test_v23_learning_source import _fixture


@pytest.mark.parametrize('mode', ['valid', 'changed', 'cancel', 'missing', 'lock'])
def test_holdout_recovers_actual_fan_confidence_and_closes_model(tmp_path,mode):
    artifacts,case=_fixture(tmp_path)
    box=FaceRect(x0=.1,y0=.1,x1=.9,y1=.9)
    preview,_=work_to_model(_preview(artifacts.rgb,2048))
    _,geometry=_crop(preview,box,256,1.25)
    raw=np.full((68,3),128,np.float32);raw[:,2]=.4
    points=_points(raw,preview.shape[1],preview.shape[0],geometry)
    face=FaceObservation(observation_id=uuid4(),face_id=uuid4(),box=box,
        landmarks=[FacePoint(x=point.x,y=point.y) for point in points],
        embedding=None,quality=1,person_id=None)
    if mode=='changed':face.landmarks[0].x+=.001
    observations=FaceObservations(schema_version=1,binding=case.binding,faces=[face])
    events=[];cancelled=False
    class Engine:
        def infer(self,crop):
            nonlocal cancelled
            events.append('infer')
            assert crop.shape==(256,256,3)
            if mode=='cancel':cancelled=True
            return raw
    @contextmanager
    def acquire(model,backend):
        events.append('open');assert model=='fan2d' and backend=='cuda'
        try:yield Engine()
        finally:events.append('close')
    pool=SimpleNamespace(lock=SimpleNamespace(lock_id='c'*64,
        models=[] if mode=='missing' else [SimpleNamespace(id='fan2d')]),acquire=acquire)
    def active():
        if cancelled:raise RuntimeError('lease lost')
    def run():return recover_holdout_landmarks(artifacts.rgb,artifacts.image,
        observations,pool,'cuda','d'*64 if mode=='lock' else 'c'*64,active)
    if mode=='valid':
        result=run();assert len(result)==1
        assert all(point.confidence==pytest.approx(.4) for point in result[0].points)
        assert result[0].canvas==artifacts.image.canvas
        assert result[0].object_id==face.face_id
    elif mode=='cancel':
        with pytest.raises(RuntimeError,match='lease lost'):run()
    else:
        with pytest.raises(ValueError,match='HOLDOUT_LANDMARK'):run()
    assert events==(['open','infer','close'] if mode in ('valid','changed','cancel') else [])


@pytest.mark.parametrize('dtype', [np.float32, np.uint16])
def test_holdout_fan_uses_describe_transparent_pixel_exclusion(tmp_path,dtype):
    artifacts,case=_fixture(tmp_path)
    alpha=np.ones(artifacts.rgb.shape[:2],dtype=dtype)
    alpha[:, :alpha.shape[1]//2]=0
    preview,_=work_to_model(_preview(artifacts.rgb,2048))
    preview=np.where(_preview(alpha,2048)[...,None]>0,preview,0).astype(np.float32)
    box=FaceRect(x0=.1,y0=.1,x1=.9,y1=.9)
    expected,geometry=_crop(preview,box,256,1.25)
    raw=np.full((68,3),128,np.float32);raw[:,2]=.9
    points=_points(raw,preview.shape[1],preview.shape[0],geometry)
    observations=FaceObservations(schema_version=1,binding=case.binding,faces=[
        FaceObservation(observation_id=uuid4(),face_id=uuid4(),box=box,
            landmarks=[FacePoint(x=p.x,y=p.y) for p in points],embedding=None,quality=1,person_id=None)])
    class Engine:
        def infer(self,crop):
            np.testing.assert_array_equal(crop,expected)
            return raw
    @contextmanager
    def acquire(*args):yield Engine()
    pool=SimpleNamespace(lock=SimpleNamespace(lock_id='c'*64,
        models=[SimpleNamespace(id='fan2d')]),acquire=acquire)
    assert len(recover_holdout_landmarks(artifacts.rgb,artifacts.image,
        observations,pool,'cuda','c'*64,alpha=alpha))==1
