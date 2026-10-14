from types import SimpleNamespace
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2 import negative_qualified_replay as module
from photo_worker.v2.negative_qualification import NegativeQualificationPin
from photo_worker.operations.optical.negative_resources import negative_resources
from test_negative_qualification import fixture
from test_negative_result import source_case


@pytest.mark.parametrize('mode',['ok','revoked','color','source','corrupt','budget'])
def test_qualified_negative_replay_binds_current_original_evidence(monkeypatch,tmp_path,mode):
    p,c,r,pin,policy=fixture();_,source,_=source_case()
    from photo_worker.contracts.optical_negative import OpticalNegativeParams
    params=OpticalNegativeParams.model_validate_json(canonical_bytes(p))
    typed=NegativeQualificationPin.from_value(pin);calls=[]
    profile=dict(backend='cpu',renderer_version='v2.2',negative_runtime_sha='b'*64,
        negative_qualification=pin,resource_estimate=negative_resources(source.canvas.width,source.canvas.height,has_alpha=source.alpha is not None))
    monkeypatch.setattr(module,'verify_negative_result_metadata',lambda *args,**kwargs:(params,object()))
    monkeypatch.setattr(module,'_document',lambda *args:source)
    def replay(*args,**kwargs):
        kwargs['check_active']();calls.append('replay');return 'verified'
    monkeypatch.setattr(module,'verify_pinned_negative_replay',replay)
    bodies={typed.calibration:c,typed.report:r}
    class Reader:
        def evidence(self,ref):return SimpleNamespace(ref=ref,source='input',media_type='application/json',bytes=len(bodies[ref]))
        def read_json(self,ref,limit):
            calls.append(ref)
            return bodies[ref]+(b' ' if mode=='corrupt' else b'')
    def run():return module.verify_qualified_negative_replay(Reader(),None,None,profile=profile,policy=policy,
        runtime_lock=object(),runtime_sha='b'*64,scratch_root=tmp_path,max_bytes=1<<30,
        ram_max_bytes=1 if mode=='budget' else 1<<30,current_pin=lambda:None if mode=='revoked' else typed,
        source_authority=lambda *args:mode!='source',verify_color=lambda image:mode!='color',check_active=lambda:None)
    if mode=='ok':assert run()=='verified' and calls==[typed.calibration,typed.report,'replay']
    else:
        with pytest.raises(ValueError):run()
        assert 'replay' not in calls
        if mode in ('revoked','color','source','budget'):assert not calls
        if mode=='corrupt':assert calls==[typed.calibration]
