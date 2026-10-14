from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_hash,ArtifactRef
from photo_worker.operations.portrait import relight_resolver as module
from test_relight_inputs import inputs
from test_portrait_manual_admission import context,ref


@pytest.mark.parametrize('failure',['','depth','subject','guard','qualification','pin','fence','inputs','unknown'])
def test_all_authorities_are_required_and_replayed_each_call(monkeypatch,failure):
    args=inputs();params,source,depth,subject,guard,calibration,declared=args
    ctx=context();ctx.lease.command.params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params))
    ctx.lease.command.inputs=list(declared);events=[]
    def check(label):
        events.append(label)
        if label==failure:return False
        if label=='guard' and failure=='fence':ctx.lease.fence+=1
        if label=='guard' and failure=='inputs':ctx.lease.command.inputs.append(ref())
        return True
    def load(*args,**kwargs):
        events.append('qualification')
        if failure=='qualification':raise RuntimeError('revoked')
        def pin():
            events.append('pin')
            if failure=='pin':raise RuntimeError('revoked')
            return {'fixture':'pin'}
        return b'report',b'calibration',{'fixture':'policy'},pin
    monkeypatch.setattr(module,'load_relight_qualification',load)
    masks={(subject.selector,False):lambda *args:check('subject'),(guard.selector,True):lambda *args:check('guard')}
    if failure=='unknown':masks.pop((subject.selector,False))
    resolver=module.RelightProducerResolver(SimpleNamespace(relight_qualification_evidence=lambda *args:None),SimpleNamespace(get_object=lambda *args:None),'test','b'*64,SimpleNamespace(verify=lambda *args:check('depth')),masks)
    if failure:
        with pytest.raises((ValueError,RuntimeError)):resolver.resolve(ctx,*args[:-1])
        if failure=='unknown':assert not events
        if failure in ('depth','subject','guard','fence','inputs'):assert 'qualification' not in events
    else:
        for _ in range(2):assert resolver.resolve(ctx,*args[:-1])==({'fixture':'pin'},b'report',b'calibration',{'fixture':'policy'})
        assert events==['depth','subject','guard','qualification','pin']*2


def test_explicit_factory_composes_concrete_attempt_authorities(monkeypatch):
    from photo_worker.operations.portrait import relight_adapter
    from photo_worker.operations.portrait.relight_admission import QualifiedRelightAdmission,LeaseGatedRelightResolver
    from photo_worker.operations.portrait.relight_depth_authority import SQLRelightDepthAuthority
    captured={}
    def install(config,factory,**kwargs):captured.update(factory=factory,kwargs=kwargs);return ['fixture-operation']
    monkeypatch.setattr(relight_adapter,'relight_operations',install)
    database=SimpleNamespace(**{name:lambda *args:None for name in ('relight_depth_origin','relight_person_origin','relight_manual_origin','relight_lease_ready','relight_source_ready','relight_qualification_evidence')})
    artifacts=SimpleNamespace(contract_bytes=lambda *args:None,contract=lambda *args:None)
    objects=SimpleNamespace(get_object=lambda *args:None)
    assert module.qualified_relight_operations(object(),database,objects,'test',{'lock':'fixture'},'b'*64)==['fixture-operation']
    admission=captured['factory'](context(),artifacts)
    assert isinstance(admission,QualifiedRelightAdmission)
    assert isinstance(admission.resolver,LeaseGatedRelightResolver)
    resolver=admission.resolver.resolver
    assert isinstance(resolver.depth,SQLRelightDepthAuthority)
    assert set(resolver.masks)=={('person',False),('manual',False),('manual',True)}
    assert captured['kwargs']==dict(runtime_lock={'lock':'fixture'},expected_runtime_sha='b'*64)
