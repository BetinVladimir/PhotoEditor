from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2 import render_derivative_service as service
from photo_worker.v2.render_derivative_adoption import verify_render_derivative_result
from photo_worker.v2.adoption import AdoptionInvalid


@pytest.mark.parametrize('profiles',[['a'*64],('bad',),('a'*64,'a'*64),([],),('a'*64,)*3])
def test_derivative_extra_profile_installation_rejected_before_io(profiles):
    class Forbidden:
        def evidence(self,*args):raise AssertionError('IO before ICC validation')
    with pytest.raises(AdoptionInvalid,match='ICC_REQUIRED'):
        verify_render_derivative_result(Forbidden(),None,None,target_icc_sha256='a'*64,
            working_icc_sha256='b'*64,additional_working_icc_sha256=profiles)


def test_derivative_coordinator_passes_only_installed_linear_pin(monkeypatch):
    task,result,params,manifest=[uuid4() for _ in range(4)]
    rows=[(task,result,1,1,params,'a'*64,manifest,'b'*64)]
    read=SimpleNamespace(autocommit=True,execute=lambda *args:SimpleNamespace(fetchall=lambda:rows))
    write=SimpleNamespace(autocommit=True)
    coordinator=service.RenderDerivativeAdoptionCoordinator(read,write,object(),'photos',
        target_icc_sha256='a'*64,working_icc_sha256='b'*64,linear_icc_sha256='c'*64)
    observed=[]
    def verify(*args,**kwargs):
        observed.append(kwargs)
        raise AdoptionInvalid('STOP_AFTER_ICC_CAPTURE')
    monkeypatch.setattr(service,'verify_render_derivative_result',verify)
    with pytest.raises(AdoptionInvalid,match='STOP_AFTER_ICC_CAPTURE'):
        coordinator.adopt(uuid4(),uuid4(),'preview1600')
    assert observed[0]['additional_working_icc_sha256']==('c'*64,)
    assert observed[0]['working_icc_sha256']=='b'*64
