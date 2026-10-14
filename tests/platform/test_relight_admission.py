from types import SimpleNamespace
from uuid import uuid4
import pytest
from test_relight_inputs import inputs
from test_relight_qualification import evidence
from photo_worker.contracts.base import canonical_bytes,canonical_hash,ArtifactRef
from photo_worker.operations.portrait.relight_admission import QualifiedRelightAdmission

@pytest.mark.parametrize('mode',['success','qualification','attempt','context','inputs','operation','version','resource','runtime'])
def test_attempt_bound_repeated_evidence(mode):
    args=inputs();params,pin,report,calibration,policy=evidence()
    # Bind independent report fixture to the actual input Calibration locator.
    pin['calibration']=args[0].calibration.model_dump(mode='json')
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(
        worker_id=uuid4(),fence=1,input_digest='a'*64,execution_profile={'relight_runtime_sha':'b'*64},
        command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=1,
            operation='image.relight',operation_version='1',resource_class='cpu',
            params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(args[0])),inputs=list(args[-1]))))
    class Resolver:
        calls=0
        def resolve(self,*unused):
            self.calls+=1
            if mode=='qualification' and self.calls==4:pin['qualification_id']=str(uuid4())
            if mode=='attempt' and self.calls==4:ctx.lease.fence=2
            if self.calls==4:
                if mode=='inputs':ctx.lease.command.inputs.pop()
                if mode=='operation':ctx.lease.command.operation='portrait.hair'
                if mode=='version':ctx.lease.command.operation_version='2'
                if mode=='resource':ctx.lease.command.resource_class='gpu'
                if mode=='runtime':ctx.lease.execution_profile['relight_runtime_sha']='0'*64
            return pin,canonical_bytes(report),canonical_bytes(calibration),policy
    resolver=Resolver();authority=QualifiedRelightAdmission(resolver,'b'*64)
    authority.admit(ctx,*args[:-1])
    if mode=='context':ctx=SimpleNamespace(lease=ctx.lease,check_active=ctx.check_active)
    if mode=='success':
        authority.admit(ctx,*args[:-1]);assert resolver.calls==4
    else:
        with pytest.raises((ValueError,RuntimeError)):authority.admit(ctx,*args[:-1])

@pytest.mark.parametrize('states',[(True,True),(False,),(True,False)])
def test_sql_gate_checks_before_and_after_resolver(states):
    from photo_worker.operations.portrait.relight_admission import LeaseGatedRelightResolver
    args=inputs();calls=[];resolved=[];remaining=iter(states)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(worker_id=uuid4(),fence=1,
        input_digest='a'*64,execution_profile={},command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),
        task_id=uuid4(),attempt=1,params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(args[0])))))
    def gate(*values):calls.append(values);return next(remaining)
    def resolve(*values):resolved.append(values);return 'evidence'
    resolver=LeaseGatedRelightResolver(SimpleNamespace(relight_lease_ready=gate,relight_source_ready=lambda *args:True),SimpleNamespace(resolve=resolve))
    if states==(True,True):assert resolver.resolve(ctx,*args[:-1])=='evidence'
    else:
        with pytest.raises(RuntimeError,match='RELIGHT_LEASE_NOT_READY'):resolver.resolve(ctx,*args[:-1])
    assert len(calls)==len(states) and len(resolved)==int(states[0])
    assert calls[0][1]==canonical_bytes(args[0])
