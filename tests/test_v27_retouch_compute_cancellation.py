"""Cancellation must precede large defensive copies and stop later allocations."""
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import parse_json
from photo_worker.contracts.images import WorkingImage, Face68
from photo_worker.operations.semantic import preview_retouch_compute as module
from photo_worker.runtime.executor import TaskCancelled, LeaseLost
from test_v27_retouch_preview_adapter_binding import fixture
from test_v27_exposure_geometry import geometry_fixture


@pytest.mark.parametrize('boundary', [1,2,3,4,5])
@pytest.mark.parametrize('failure', [TaskCancelled,LeaseLost])
def test_compute_stops_defensive_allocations_at_each_activity_boundary(monkeypatch,boundary,failure):
    params,_,_,_,raw,_=fixture();context=parse_json(raw)
    edit,_,_,source_raw,*_=geometry_fixture()
    params['source']=edit['source'];context['source']=edit['source']
    image=WorkingImage.model_validate_json(source_raw)
    face=Face68.model_validate(dict(object_id=uuid4(),points=[dict(x=.5,y=.5,confidence=.9) for _ in range(68)],
        canvas=image.canvas,transform_chain=image.transform_chain))
    allocations=[];checks=0
    class Tracked(np.ndarray):
        def copy(self,*args,**kwargs):
            allocations.append('rgb')
            return np.asarray(self).copy(*args,**kwargs)
        def astype(self,*args,**kwargs):
            allocations.append('mask')
            return np.asarray(self).astype(*args,**kwargs)
    rgb=np.full((8,8,3),.5,dtype=np.float32).view(Tracked)
    mask=np.ones((8,8),dtype=np.float32).view(Tracked)
    protected=np.zeros((8,8),dtype=np.float32).view(Tracked)
    def active():
        nonlocal checks
        checks+=1
        if checks==boundary:raise failure('stopped before next allocation')
    def forbidden(*args,**kwargs):raise AssertionError('stopped task reached authority or beauty')
    monkeypatch.setattr(module,'prepare_beauty',forbidden)
    with pytest.raises(failure):
        with module.compute_retouch_preview(params,context,image,rgb,mask,protected,(face,),object(),
            strength=.5,max_ram_bytes=2**31,verify_admission=forbidden,check_active=active):
            raise AssertionError('stopped compute yielded output')
    assert allocations==['rgb','mask','mask'][:min(boundary-1,3)]
    assert checks==boundary
