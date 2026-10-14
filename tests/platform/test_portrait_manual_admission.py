from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.portrait import local_manual_admission as module
from photo_worker.operations.portrait.local_manual_inputs import ManualPairOrigin
from photo_worker.runtime.executor import OperationFailure


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


def context():
    command=SimpleNamespace(inputs=[],tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=1,params=ref())
    return SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command,worker_id=uuid4(),fence=1,input_digest='b'*64,execution_profile={'backend':'cpu','renderer_version':'v2.2','portrait_runtime_sha':'e'*64,'resource_estimate':{'ram_bytes':1024}}))


@pytest.mark.parametrize('changed',[False,True])
def test_manual_admission_rechecks_origin_after_physical_replay(monkeypatch,changed):
    events=[]
    origin=ManualPairOrigin(ref(),ref(),None)
    other=ManualPairOrigin(ref(),origin.params,None)
    class Authority:
        calls=0
        def resolve(self,*args):
            self.calls+=1;events.append('resolve')
            return other if changed and self.calls==2 else origin
    monkeypatch.setattr(module,'verify_local_inputs',lambda *args:events.append('closure'))
    monkeypatch.setattr(module,'read_verified_manual_pair',lambda *args:events.append('raster'))
    ctx=context()
    params=SimpleNamespace(base=SimpleNamespace(source=ref()))
    mask=SimpleNamespace(selector='manual')
    admission=module.ManualLocalPortraitAdmission(object(),Authority())
    if changed:
        with pytest.raises(OperationFailure,match='PORTRAIT_MANUAL_ORIGIN_CHANGED'):
            admission.admit(ctx,params,object(),mask,mask)
    else:admission.admit(ctx,params,object(),mask,mask)
    assert events==['closure','resolve','raster','resolve']


def test_manual_admission_requires_concrete_authority():
    for authority in (None,object(),SimpleNamespace(resolve=None)):
        with pytest.raises(ValueError,match='PORTRAIT_MANUAL_ADMISSION_CONFIG_INVALID'):
            module.ManualLocalPortraitAdmission(object(),authority)


def test_manual_admission_cancellation_prevents_all_reads():
    class Authority:
        def resolve(self,*args):raise AssertionError('unexpected authority')
    def cancelled():raise RuntimeError('cancelled')
    admission=module.ManualLocalPortraitAdmission(object(),Authority())
    with pytest.raises(RuntimeError,match='cancelled'):
        admission.admit(SimpleNamespace(check_active=cancelled),None,None,None,None)


@pytest.mark.parametrize('failure',['selector','origin','raster'])
def test_manual_admission_refuses_unproven_masks_before_second_authority(monkeypatch,failure):
    events=[]
    class Authority:
        def resolve(self,*args):
            events.append('resolve')
            return None if failure=='origin' else ManualPairOrigin(ref(),ref(),None)
    def replay(*args):
        events.append('raster')
        raise ValueError('PORTRAIT_MANUAL_RASTER_MISMATCH')
    monkeypatch.setattr(module,'verify_local_inputs',lambda *args:None)
    monkeypatch.setattr(module,'read_verified_manual_pair',replay)
    ctx=context()
    params=SimpleNamespace(base=SimpleNamespace(source=ref()))
    mask=SimpleNamespace(selector='hair' if failure=='selector' else 'manual')
    admission=module.ManualLocalPortraitAdmission(object(),Authority())
    with pytest.raises((ValueError,OperationFailure)):
        admission.admit(ctx,params,object(),mask,mask)
    assert events=={'selector':[],'origin':['resolve'],'raster':['resolve','raster']}[failure]


@pytest.mark.parametrize('change',['root','params','vectors','context','none'])
def test_manual_admission_pins_origin_across_prepare_and_publication(monkeypatch,change):
    origin=ManualPairOrigin(ref(),ref(),None)
    next_origin={'root':ManualPairOrigin(ref(),origin.params,None),'params':ManualPairOrigin(origin.root,ref(),None),'vectors':ManualPairOrigin(origin.root,origin.params,ref()),'context':origin,'none':origin}[change]
    events=[]
    class Authority:
        def resolve(self,*args):events.append('resolve');return origin if len(events)<=3 else next_origin
    monkeypatch.setattr(module,'verify_local_inputs',lambda *args:None)
    monkeypatch.setattr(module,'read_verified_manual_pair',lambda *args:events.append('raster'))
    ctx=context()
    params=SimpleNamespace(base=SimpleNamespace(source=ref()));mask=SimpleNamespace(selector='manual')
    admission=module.ManualLocalPortraitAdmission(object(),Authority())
    admission.admit(ctx,params,object(),mask,mask)
    if change=='none':
        admission.admit(ctx,params,object(),mask,mask)
        assert events==['resolve','raster','resolve']*2
    else:
        repeated_ctx=SimpleNamespace(**vars(ctx)) if change=='context' else ctx
        with pytest.raises(OperationFailure,match='PORTRAIT_MANUAL_ATTEMPT_MISMATCH' if change=='context' else 'PORTRAIT_MANUAL_ORIGIN_CHANGED'):
            admission.admit(repeated_ctx,params,object(),mask,mask)
        assert events==['resolve','raster','resolve']+([] if change=='context' else ['resolve'])


@pytest.mark.parametrize('field',['fence','attempt','task_id','params','input_digest','runtime','resources'])
def test_manual_admission_rejects_attempt_change_on_same_context(monkeypatch,field):
    origin=ManualPairOrigin(ref(),ref(),None)
    events=[]
    class Authority:
        def resolve(self,*args):events.append('resolve');return origin
    monkeypatch.setattr(module,'verify_local_inputs',lambda *args:None)
    monkeypatch.setattr(module,'read_verified_manual_pair',lambda *args:events.append('raster'))
    ctx=context();params=SimpleNamespace(base=SimpleNamespace(source=ref()));mask=SimpleNamespace(selector='manual')
    admission=module.ManualLocalPortraitAdmission(object(),Authority())
    admission.admit(ctx,params,object(),mask,mask)
    if field=='fence':ctx.lease.fence=2
    elif field=='attempt':ctx.lease.command.attempt=2
    elif field=='task_id':ctx.lease.command.task_id=uuid4()
    elif field=='params':ctx.lease.command.params=ref()
    elif field=='input_digest':ctx.lease.input_digest='c'*64
    elif field=='runtime':ctx.lease.execution_profile['portrait_runtime_sha']='f'*64
    else:ctx.lease.execution_profile['resource_estimate']['ram_bytes']=2048
    with pytest.raises(OperationFailure,match='PORTRAIT_MANUAL_ATTEMPT_MISMATCH'):
        admission.admit(ctx,params,object(),mask,mask)
    assert events==['resolve','raster','resolve']



def test_manual_admission_refuses_fence_change_during_replay(monkeypatch):
    origin=ManualPairOrigin(ref(),ref(),None)
    class Authority:
        def resolve(self,*args):return origin
    ctx=context()
    monkeypatch.setattr(module,'verify_local_inputs',lambda *args:None)
    def replay(*args):ctx.lease.fence=2
    monkeypatch.setattr(module,'read_verified_manual_pair',replay)
    admission=module.ManualLocalPortraitAdmission(object(),Authority())
    with pytest.raises(OperationFailure,match='PORTRAIT_MANUAL_ATTEMPT_MISMATCH'):
        admission.admit(ctx,SimpleNamespace(base=SimpleNamespace(source=ref())),object(),SimpleNamespace(selector='manual'),SimpleNamespace(selector='manual'))
    assert admission._origin is None
